package node

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"p2pnode/internal/protocol"
	"p2pnode/internal/record"
	"p2pnode/internal/routing"
	"p2pnode/internal/rpc"
	"p2pnode/internal/store"
	"p2pnode/internal/tunnel"
)

func (n *Node) initTunnel() error {
	localAddr := n.localAddr()

	rb := tunnel.NewRouteBuilder(
		n.Identity.NodeID,
		localAddr,
		uint8(n.Config.MaxHops),
		&nodeRouteSource{n: n},
		n.profileStore,
	)
	rb.Log = n.Log

	n.tunnelMgr = tunnel.NewTunnelManager(
		n.Identity.NodeID,
		localAddr,
		n.Identity.PublicKey,
		n.Identity.PrivateKey,
		n.tunnelDial,
		n.findDestForTunnel,
		rb,
		n.profileStore,
		n.Log,
	)
	n.tunnelMgr.PoolSize = n.Config.TunnelPoolSize
	n.tunnelMgr.AckTimeout = time.Duration(n.Config.TunnelAckTimeoutMs) * time.Millisecond
	n.tunnelMgr.TTL = time.Duration(n.Config.TunnelTTLSec) * time.Second

	n.tunnelHandlers = &tunnel.HandlerConfig{
		LocalID:      n.Identity.NodeID,
		LocalAddr:    localAddr,
		LocalPubKey:  n.Identity.PublicKey,
		LocalPrivKey: n.Identity.PrivateKey,
		Dial:         n.tunnelDial,
		Log:          n.Log,
		RelayStore:   tunnel.NewRelayStore(),
		DestSessions: tunnel.NewDestSessionStore(),
		Trace:        n.tunnelMgr.Trace,
	}

	n.tunnelMgr.SetOnMessage(func(msg tunnel.Message) {
		n.Log.Info("node: incoming message",
			"from", fmt.Sprintf("%x", msg.FromID[:4]),
			"text_len", len(msg.Text))
		n.msgHistory.Add(msg)
		if n.onMessage != nil {
			n.onMessage(msg)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	n.tunnelCtx = ctx
	n.tunnelCancel = cancel

	tunnel.StartStoreExpireLoop(
		ctx,
		n.tunnelHandlers.RelayStore,
		n.tunnelHandlers.DestSessions,
		n.Log,
	)
	n.tunnelMgr.StartExpireLoop(ctx)

	n.Log.Info("node: tunnel subsystem initialized",
		"max_hops", n.Config.MaxHops,
		"pool_size", n.Config.TunnelPoolSize,
		"ack_timeout_ms", n.Config.TunnelAckTimeoutMs,
		"ttl_sec", n.Config.TunnelTTLSec)
	return nil
}

func (n *Node) SendMessage(destID [32]byte, text string) (tunnel.MessageID, error) {
	if n.tunnelMgr == nil {
		return tunnel.MessageID{}, fmt.Errorf("node: tunnel not initialized")
	}
	return n.tunnelMgr.SendMessage(destID, text)
}

func (n *Node) BuildTunnelPool(destID [32]byte) error {
	if n.tunnelMgr == nil {
		return fmt.Errorf("node: tunnel not initialized")
	}
	res := n.Client.LookupNode(
		n.Local,
		n.Table,
		n.Identity.NodeID,
		rpc.LookupConfig{
			Alpha:   n.Config.Alpha,
			K:       n.Config.KBucketSize,
			Timeout: n.Config.PingTimeout,
		},
	)
	n.Log.Info("pool: routing table enriched",
		"table_size", n.Table.Size(),
		"rpc", res.RPC,
		"iterations", res.Iterations)
	_, err := n.tunnelMgr.BuildPool(destID)
	return err
}

func (n *Node) OnMessage(cb func(tunnel.Message)) {
	n.onMessage = cb
}

func (n *Node) History() []tunnel.Message {
	return n.msgHistory.All()
}

func (n *Node) TunnelStats() tunnel.ManagerStats {
	if n.tunnelMgr == nil {
		return tunnel.ManagerStats{}
	}
	return n.tunnelMgr.Stats()
}

func (n *Node) handleTunnelFrame(conn tunnel.RawConn, frame protocol.Frame) error {
	if n.tunnelHandlers == nil {
		return fmt.Errorf("node: tunnel handlers not initialized")
	}

	switch frame.Type {
	case protocol.MsgTunnelBuild:
		return n.tunnelHandlers.HandleTunnelBuild(conn, frame)
	case protocol.MsgTunnelData:
		n.Log.Debug("node: unexpected TUNNEL_DATA in dispatcher")
		return nil
	case protocol.MsgTunnelBuildOK, protocol.MsgTunnelBuildFail,
		protocol.MsgTunnelBuildAck, protocol.MsgTunnelAck,
		protocol.MsgTunnelClose:
		n.Log.Debug("node: TUNNEL_* handled by relay/session loop",
			"type", frame.Type)
		return nil
	default:
		return fmt.Errorf("node: unhandled tunnel frame type %v", frame.Type)
	}
}

func (n *Node) tunnelDial(addr string) (tunnel.RawConn, error) {
	conn, err := n.trTunnel.Dial(addr)
	if err != nil {
		return nil, err
	}
	raw, ok := conn.(tunnel.RawConn)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("node: conn does not implement tunnel.RawConn")
	}
	return raw, nil
}

func (n *Node) findDestForTunnel(destID [32]byte) (ed25519.PublicKey, string, error) {
	key := NodeKeyForID(record.NodeID(destID))

	now := time.Now()
	if rec, ok := n.Store.Get(key, now); ok && rec != nil {
		return extractPubAddr(rec)
	}

	rec, err := n.FindValue(key)
	if err != nil {
		return nil, "", fmt.Errorf("node: find dest record: %w", err)
	}
	return extractPubAddr(rec)
}

func (n *Node) localAddr() string {
	return fmt.Sprintf("%s:%d", n.Local.Host, n.Local.Port)
}

func extractPubAddr(rec *record.NodeRecord) (ed25519.PublicKey, string, error) {
	if rec == nil {
		return nil, "", fmt.Errorf("node: nil record")
	}
	if len(rec.IdentityPubKey) != ed25519.PublicKeySize {
		return nil, "", fmt.Errorf("node: bad pubkey size %d", len(rec.IdentityPubKey))
	}
	if len(rec.Addresses) == 0 {
		return nil, "", fmt.Errorf("node: no addresses in record")
	}
	pub := ed25519.PublicKey(rec.IdentityPubKey)
	return pub, rec.Addresses[0], nil
}

type nodeRouteSource struct {
	n *Node
}

func (s *nodeRouteSource) FindDest(destID [32]byte) (string, []byte, error) {
	pub, addr, err := s.n.findDestForTunnel(destID)
	if err != nil {
		return "", nil, err
	}
	return addr, pub, nil
}

func (s *nodeRouteSource) KnownPeers() [][32]byte {
	contacts := s.n.Table.Snapshot()
	out := make([][32]byte, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, c.NodeID)
	}
	return out
}

func (s *nodeRouteSource) AddrOf(nodeID [32]byte) (string, bool) {
	contacts := s.n.Table.Snapshot()
	for _, c := range contacts {
		if c.NodeID == nodeID {
			return fmt.Sprintf("%s:%d", c.Host, c.Port), true
		}
	}
	return "", false
}

func (s *nodeRouteSource) Ping(addr string) (time.Duration, error) {
	start := time.Now()
	_, err := s.n.Client.Ping(s.n.Local, addr, nil, s.n.Config.PingTimeout)
	if err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

func (n *Node) ExportTunnelTrace(path string) error {
	if n.tunnelMgr == nil || n.tunnelMgr.Trace == nil {
		return fmt.Errorf("node: tunnel manager not initialized")
	}
	return n.tunnelMgr.Trace.ExportJSON(path)
}

var (
	_ = routing.Contact{}
	_ = store.ID{}
)
