package node

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"p2pnode/internal/config"
	"p2pnode/internal/events"
	"p2pnode/internal/filetransfer"
	"p2pnode/internal/identity"
	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/rpc"
	"p2pnode/internal/store"
	"p2pnode/internal/transport"
	"p2pnode/internal/transport/tcp"
	"p2pnode/internal/tunnel"
)

type Node struct {
	Config   config.Config
	Log      *slog.Logger
	Identity *identity.Identity
	Local    routing.Contact

	Table    *routing.RoutingTable
	Store    *store.Store
	Client   *rpc.Client
	Server   *rpc.Server
	Checker  *rpc.PingChecker
	Listener transport.Listener

	Events *events.Logger

	trTunnel transport.Transport

	ExpireInterval    time.Duration
	RepublishInterval time.Duration
	RefreshInterval   time.Duration

	tunnelMgr      *tunnel.TunnelManager
	tunnelHandlers *tunnel.HandlerConfig
	profileStore   *tunnel.ProfileStore
	msgHistory     *tunnel.MessageStore
	onMessage      func(tunnel.Message)

	tunnelCtx    context.Context
	tunnelCancel context.CancelFunc

	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWG     sync.WaitGroup
}

func New(cfg config.Config, log *slog.Logger) (*Node, error) {
	id, err := identity.LoadOrGenerate(cfg.NodeStateDir)
	if err != nil {
		return nil, fmt.Errorf("node: identity: %w", err)
	}

	eventsPath := filepath.Join(cfg.NodeStateDir, "events.jsonl")
	ev, err := events.NewLogger(eventsPath, id.NodeID.Short())
	if err != nil {
		log.Warn("events: logger disabled", "err", err)
		ev, _ = events.NewLogger("", id.NodeID.Short())
	}

	ev.Log("identity_loaded", map[string]any{
		"node_id":   id.NodeID.String(),
		"state_dir": cfg.NodeStateDir,
	})

	tr := tcp.NewWithOptions(tcp.Options{
		ConnectTimeout: cfg.ConnectTimeout,
		ReadTimeout:    cfg.ReadTimeout,
	})

	tunnelReadTimeout := time.Duration(cfg.TunnelTTLSec)*time.Second + 30*time.Second
	if tunnelReadTimeout <= 30*time.Second {
		tunnelReadTimeout = 5*time.Minute + 30*time.Second
	}
	trTunnel := tcp.NewWithOptions(tcp.Options{
		ConnectTimeout: cfg.ConnectTimeout,
		ReadTimeout:    tunnelReadTimeout,
	})

	ln, err := tr.Listen(cfg.ListenAddr())
	if err != nil {
		ev.Close()
		return nil, fmt.Errorf("node: listen %s: %w", cfg.ListenAddr(), err)
	}

	host, port, err := splitListenAddr(ln.Addr())
	if err != nil {
		ln.Close()
		ev.Close()
		return nil, fmt.Errorf("node: parse listen addr: %w", err)
	}

	host = cfg.AdvertiseHostOr(host)

	now := uint64(time.Now().UnixMilli())
	local := routing.Contact{
		NodeID:            id.NodeID,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: id.PublicKey,
		Host:              host,
		Port:              port,
		LastSeenMs:        now,
		LastVerifiedMs:    now,
	}

	table := routing.NewRoutingTable(id.NodeID, cfg.KBucketSize)
	table.SetObserver(NewRoutingObserver(ev))
	table.SetSubnetDiversityLimit(cfg.SubnetDiversityLimit)

	client := rpc.NewClient(tr, log, ev, id)
	checker := rpc.NewPingChecker(client, local, cfg.PingTimeout)
	table.SetLivenessChecker(checker)
	st := store.New()
	srv := rpc.NewServer(local, id, table, st, checker, log, ev)
	srv.HandshakeTimeout = cfg.HandshakeTimeout

	if cfg.SybilMode {
		srv.SybilConfig = &rpc.SybilConfig{
			Enabled: true,
			Victim:  strings.ToLower(cfg.SybilVictim),
		}
		log.Warn("node: SYBIL MODE ENABLED",
			"victim", cfg.SybilVictim)
	}

	ev.Log("server_started", map[string]any{
		"addr":           ln.Addr(),
		"advertise_host": host,
	})

	n := &Node{
		Config:            cfg,
		Log:               log,
		Identity:          id,
		Local:             local,
		Table:             table,
		Store:             st,
		Client:            client,
		Server:            srv,
		Checker:           checker,
		Listener:          ln,
		Events:            ev,
		trTunnel:          trTunnel,
		ExpireInterval:    ExpireInterval,
		RepublishInterval: RepublishInterval,
		RefreshInterval:   cfg.RefreshInterval,
	}

	n.profileStore = tunnel.NewProfileStore()
	n.msgHistory = tunnel.NewMessageStore(tunnel.DefaultMessageStoreSize)

	if err := n.initTunnel(); err != nil {
		ln.Close()
		ev.Close()
		return nil, fmt.Errorf("node: init tunnel: %w", err)
	}

	srv.TunnelHandler = func(conn transport.Conn, frame protocol.Frame) error {
		raw, ok := conn.(tunnel.RawConn)
		if !ok {
			return fmt.Errorf("node: conn does not implement tunnel.RawConn")
		}
		return n.handleTunnelFrame(raw, frame)
	}

	return n, nil
}

func (n *Node) nodeCtx() context.Context {
	if n.bgCtx != nil {
		return n.bgCtx
	}
	return context.Background()
}

func (n *Node) Start() {
	go n.Server.Serve(n.Listener)
}

func (n *Node) Stop() error {
	n.Log.Info("node: stopping...")
	start := time.Now()

	n.Log.Debug("node: closing listener")
	err := n.Listener.Close()

	n.Log.Debug("node: stopping background tasks")
	n.stopBackgroundTasks()
	n.Log.Debug("node: background tasks stopped", "elapsed_ms", time.Since(start).Milliseconds())

	if n.Checker != nil {
		n.Checker.Close()
	}

	if n.Events != nil {
		n.Log.Debug("node: closing events logger")
		_ = n.Events.Close()
	}

	if n.tunnelMgr != nil && n.tunnelMgr.Trace != nil {
		tracePath := filepath.Join(n.Config.NodeStateDir, "trace.json")
		if err := n.tunnelMgr.Trace.ExportJSON(tracePath); err != nil {
			n.Log.Warn("node: export trace", "path", tracePath, "err", err)
		} else {
			n.Log.Debug("node: trace exported",
				"path", tracePath,
				"events", n.tunnelMgr.Trace.Count())
		}
	}

	if n.tunnelMgr != nil {
		n.Log.Debug("node: closing tunnel manager")
		n.tunnelMgr.Close()
		n.Log.Debug("node: tunnel manager closed")
	}
	if n.tunnelHandlers != nil && n.tunnelHandlers.RelayStore != nil {
		n.Log.Debug("node: closing relay store")
		n.tunnelHandlers.RelayStore.Close()
	}
	if n.tunnelHandlers != nil && n.tunnelHandlers.DestSessions != nil {
		n.tunnelHandlers.DestSessions.ExpireAll()
	}

	if n.Events != nil {
		n.Log.Debug("node: closing events logger")
		_ = n.Events.Close()
	}

	n.Log.Info("node: stopped", "total_ms", time.Since(start).Milliseconds())
	return err
}

func (n *Node) Bootstrap() error {
	if len(n.Config.BootstrapPeers) == 0 {
		n.Log.Info("bootstrap: skipped (no peers)")
		return nil
	}

	ctx := n.nodeCtx()

	for _, addr := range n.Config.BootstrapPeers {
		n.Log.Info("bootstrap: pinging seed", "addr", addr)
		seed, err := n.Client.Ping(ctx, n.Local, addr, nil, n.Config.PingTimeout)
		if err != nil {
			n.Log.Warn("bootstrap: ping seed failed", "addr", addr, "err", err)
			continue
		}
		n.Log.Info("bootstrap: seed identified",
			"node_id", seed.NodeID.Short(),
			"addr", seed.Addr())

		if n.Config.SkipSelfLookup {
			nodes, err := n.Client.FindNodeRPC(ctx, n.Local, seed, n.Identity.NodeID, n.Config.RPCTimeout)
			if err != nil {
				n.Log.Warn("bootstrap: find_node from seed failed", "err", err)
				continue
			}
			added := 0
			for _, c := range nodes {
				if c.NodeID == n.Identity.NodeID {
					continue
				}
				if n.Table.Add(c, n.Checker) {
					added++
				}
			}
			n.Log.Info("bootstrap: skip-self-lookup done",
				"received", len(nodes),
				"added", added,
				"table_size", n.Table.Size())
			if n.Table.Size() > 0 {
				return nil
			}
			continue
		}

		n.Table.Add(seed, n.Checker)

		result := n.Client.LookupNode(
			ctx,
			n.Local,
			n.Table,
			n.Identity.NodeID,
			rpc.LookupConfig{
				Alpha:         n.Config.Alpha,
				K:             n.Config.KBucketSize,
				Timeout:       n.Config.RPCTimeout,
				LookupTimeout: n.Config.LookupTimeout,
			},
		)
		n.Log.Info("bootstrap: self-lookup done",
			"rpc", result.RPC,
			"iterations", result.Iterations,
			"duration_ms", result.Duration.Milliseconds(),
			"table_size", n.Table.Size())
	}

	if n.Table.Size() == 0 {
		return fmt.Errorf("node: bootstrap failed: routing table is empty (all seeds unreachable?)")
	}
	return nil
}

func (n *Node) SnapshotContacts() []routing.Contact {
	return n.Table.Snapshot()
}

func splitListenAddr(addr string) (string, uint16, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, uint16(p), nil
}

func (n *Node) SendFile(
	ctx context.Context,
	destID [32]byte,
	path string,
	chunkSize uint32,
) (*filetransfer.Header, filetransfer.Progress, error) {
	if n.tunnelMgr == nil {
		return nil, filetransfer.Progress{},
			fmt.Errorf("node: tunnel manager not initialized")
	}
	return n.tunnelMgr.SendFile(ctx, destID, path, chunkSize)
}

func (n *Node) StartFileReceiver(ctx context.Context, dir string) error {
	if n.tunnelMgr == nil {
		return fmt.Errorf("node: tunnel manager not initialized")
	}
	return n.tunnelMgr.StartFileReceiver(ctx, dir)
}

func (n *Node) LookupNodeMultiPath(target routing.ID) (rpc.MultiPathResult, error) {
	ctx := n.nodeCtx()

	paths := n.Config.LookupPaths
	if paths <= 1 || len(n.Config.BootstrapPeers) == 0 {
		res := n.Client.LookupNode(ctx, n.Local, n.Table, target, rpc.LookupConfig{
			Alpha:         n.Config.Alpha,
			K:             n.Config.KBucketSize,
			Timeout:       n.Config.RPCTimeout,
			LookupTimeout: n.Config.LookupTimeout,
		})
		return rpc.MultiPathResult{
			Target:    target,
			Paths:     []rpc.LookupResult{res},
			PathCount: 1,
			Winner:    rpc.ExtractTargetAddr(res),
			Agreement: 1,
			Duration:  res.Duration,
		}, nil
	}

	seeds := make([]routing.Contact, 0, len(n.Config.BootstrapPeers))
	for _, addr := range n.Config.BootstrapPeers {
		seed, err := n.Client.Ping(ctx, n.Local, addr, nil, n.Config.PingTimeout)
		if err != nil {
			n.Log.Warn("multipath: ping seed failed", "addr", addr, "err", err)
			continue
		}
		seeds = append(seeds, seed)
	}

	if len(seeds) == 0 {
		return rpc.MultiPathResult{}, fmt.Errorf("multipath: no live seeds")
	}

	pingFn := func(seed routing.Contact) (*routing.RoutingTable, error) {
		tbl := routing.NewRoutingTable(n.Identity.NodeID, n.Config.KBucketSize)
		tbl.SetSubnetDiversityLimit(n.Config.SubnetDiversityLimit)
		tbl.Add(seed, nil)
		return tbl, nil
	}

	result := n.Client.LookupNodeMultiPath(
		ctx,
		n.Local,
		seeds,
		target,
		paths,
		rpc.LookupConfig{
			Alpha:         n.Config.Alpha,
			K:             n.Config.KBucketSize,
			Timeout:       n.Config.RPCTimeout,
			LookupTimeout: n.Config.LookupTimeout,
		},
		pingFn,
	)
	return result, nil
}
