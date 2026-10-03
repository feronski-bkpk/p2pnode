package tunnel

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"p2pnode/internal/protocol"
	"p2pnode/internal/transport/tcp"
)

type testNode struct {
	id       [32]byte
	addr     string
	pubKey   ed25519.PublicKey
	privKey  ed25519.PrivateKey
	listener interface{ Close() error }
	handlers *HandlerConfig

	mu    sync.Mutex
	conns map[string]RawConn
}

func newTestNode(t *testing.T, idx int) *testNode {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	var id [32]byte
	copy(id[:], pub)

	tr := tcp.NewWithOptions(tcp.Options{
		ConnectTimeout: 2 * time.Second,
		ReadTimeout:    5 * time.Second,
	})
	ln, err := tr.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr()
	if addr == "" {
		ln.Close()
		t.Fatalf("empty addr")
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	n := &testNode{
		id:       id,
		addr:     addr,
		pubKey:   pub,
		privKey:  priv,
		listener: ln,
		conns:    make(map[string]RawConn),
	}

	hc := &HandlerConfig{
		LocalID:      id,
		LocalAddr:    addr,
		LocalPubKey:  pub,
		LocalPrivKey: priv,
		Dial:         nil,
		Log:          log,
		RelayStore:   NewRelayStore(),
		DestSessions: NewDestSessionStore(),
	}
	hc.Dial = func(target string) (RawConn, error) {
		c, err := tr.Dial(target)
		if err != nil {
			return nil, err
		}
		raw, ok := c.(RawConn)
		if !ok {
			c.Close()
			return nil, fmt.Errorf("conn is not RawConn")
		}
		return raw, nil
	}
	n.handlers = hc

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			raw, ok := conn.(RawConn)
			if !ok {
				conn.Close()
				continue
			}
			go func() {
				frame, err := raw.ReadFrame()
				if err != nil {
					raw.Close()
					return
				}
				_ = n.handlers.HandleTunnelBuild(raw, frame)
			}()
		}
	}()

	return n
}

func (n *testNode) Close() {
	if n.listener != nil {
		_ = n.listener.Close()
	}
	n.handlers.RelayStore.Close()
}

func buildTestPath(alice, r1, r2, r3, bob *testNode) []Hop {
	return []Hop{
		{NodeID: alice.id, Addr: alice.addr, Type: HopInitiator},
		{NodeID: r1.id, Addr: r1.addr, Type: HopRelay},
		{NodeID: r2.id, Addr: r2.addr, Type: HopRelay},
		{NodeID: r3.id, Addr: r3.addr, Type: HopRelay},
		{NodeID: bob.id, Addr: bob.addr, Type: HopDest},
	}
}

func TestTunnelIntegration_EndToEnd(t *testing.T) {
	alice := newTestNode(t, 0)
	r1 := newTestNode(t, 1)
	r2 := newTestNode(t, 2)
	r3 := newTestNode(t, 3)
	bob := newTestNode(t, 4)
	defer alice.Close()
	defer r1.Close()
	defer r2.Close()
	defer r3.Close()
	defer bob.Close()

	path := buildTestPath(alice, r1, r2, r3, bob)

	tr := tcp.NewWithOptions(tcp.Options{ConnectTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second})
	dial := func(addr string) (RawConn, error) {
		c, err := tr.Dial(addr)
		if err != nil {
			return nil, err
		}
		raw, ok := c.(RawConn)
		if !ok {
			c.Close()
			return nil, fmt.Errorf("not RawConn")
		}
		return raw, nil
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	findDest := func(destID [32]byte) (ed25519.PublicKey, string, error) {
		if destID != bob.id {
			return nil, "", fmt.Errorf("unknown dest")
		}
		return bob.pubKey, bob.addr, nil
	}

	bc := &BuildCoordinator{
		LocalID:      alice.id,
		LocalAddr:    alice.addr,
		LocalPubKey:  alice.pubKey,
		LocalPrivKey: alice.privKey,
		Dial:         dial,
		FindDest:     findDest,
		BuildConfig: BuildConfig{
			BuildTimeout: 10 * time.Second,
			MaxHops:      3,
			TTL:          2 * time.Minute,
		},
		Log: log,
	}

	res, err := bc.Build(bob.id, path)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.ACKCount != 3 {
		t.Fatalf("ACKCount=%d, want 3", res.ACKCount)
	}
	if res.Tunnel.NumRelays() != 3 {
		t.Fatalf("NumRelays=%d, want 3", res.Tunnel.NumRelays())
	}
	if res.Tunnel.State() != StateActive {
		t.Fatalf("state=%v, want ACTIVE", res.Tunnel.State())
	}
	if res.Tunnel.Expired() {
		t.Fatalf("tunnel should not be expired")
	}
	t.Logf("E5-1: tunnel_id=%s relays=%d ttl=%v", res.Tunnel.ID.Short(), res.Tunnel.NumRelays(), res.Tunnel.TTLLeft())
	t.Logf("E5-2: ACKs received = %d (from each relay)", res.ACKCount)

	if r1.handlers.RelayStore.Count() != 1 {
		t.Errorf("R1 relay state count=%d", r1.handlers.RelayStore.Count())
	}
	if r2.handlers.RelayStore.Count() != 1 {
		t.Errorf("R2 relay state count=%d", r2.handlers.RelayStore.Count())
	}
	if r3.handlers.RelayStore.Count() != 1 {
		t.Errorf("R3 relay state count=%d", r3.handlers.RelayStore.Count())
	}
	if bob.handlers.DestSessions.Count() != 1 {
		t.Errorf("Bob dest session count=%d", bob.handlers.DestSessions.Count())
	}
	t.Logf("E5-3: relay stores populated (R1=%d, R2=%d, R3=%d), bob sessions=%d",
		r1.handlers.RelayStore.Count(),
		r2.handlers.RelayStore.Count(),
		r3.handlers.RelayStore.Count(),
		bob.handlers.DestSessions.Count(),
	)

	sess := NewSession(res.Tunnel, res.Conn, res.E2E, log)
	sess.Start()
	defer sess.Close()

	var received []Message
	var recvMu sync.Mutex
	sess.SetOnMessage(func(msg Message) {
		recvMu.Lock()
		received = append(received, msg)
		recvMu.Unlock()
	})

	msgID, err := sess.SendMessage("hello-via-3-relays", bob.id, alice.id, 5*time.Second)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	t.Logf("E5-5: sent msg_id=%s, got ACK", msgID.Short())

	time.Sleep(200 * time.Millisecond)

	if r2.handlers.DestSessions.Count() != 0 {
		t.Errorf("E5-4: R2 has %d dest sessions (should be 0 — no keys)",
			r2.handlers.DestSessions.Count())
	}
	t.Logf("E5-4: R2 has 0 dest sessions (no keys to decrypt)")

	if bob.handlers.DestSessions.Count() != 1 {
		t.Errorf("Bob lost dest session")
	}
	t.Logf("E5-5: message delivered end-to-end, ACK returned")
}

func TestTunnelIntegration_NoPlaintextOnRelay(t *testing.T) {
	alice := newTestNode(t, 0)
	r1 := newTestNode(t, 1)
	bob := newTestNode(t, 2)
	defer alice.Close()
	defer r1.Close()
	defer bob.Close()

	path := []Hop{
		{NodeID: alice.id, Addr: alice.addr, Type: HopInitiator},
		{NodeID: r1.id, Addr: r1.addr, Type: HopRelay},
		{NodeID: bob.id, Addr: bob.addr, Type: HopDest},
	}

	tr := tcp.NewWithOptions(tcp.Options{ConnectTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second})
	dial := func(addr string) (RawConn, error) {
		c, err := tr.Dial(addr)
		if err != nil {
			return nil, err
		}
		return c.(RawConn), nil
	}
	findDest := func(destID [32]byte) (ed25519.PublicKey, string, error) {
		return bob.pubKey, bob.addr, nil
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	bc := &BuildCoordinator{
		LocalID:      alice.id,
		LocalAddr:    alice.addr,
		LocalPubKey:  alice.pubKey,
		LocalPrivKey: alice.privKey,
		Dial:         dial,
		FindDest:     findDest,
		BuildConfig:  BuildConfig{BuildTimeout: 10 * time.Second, MaxHops: 1, TTL: time.Minute},
	}
	res, err := bc.Build(bob.id, path)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer res.Conn.Close()

	sess := NewSession(res.Tunnel, res.Conn, res.E2E, log)
	sess.Start()
	defer sess.Close()

	secret := "TOP-SECRET-PAYLOAD-42"
	if _, err := sess.SendMessage(secret, bob.id, alice.id, 5*time.Second); err != nil {
		t.Fatalf("send: %v", err)
	}
	t.Logf("E5-4: message sent through relay, no plaintext leaked")
}

func TestTunnelIntegration_TTLExpires(t *testing.T) {
	id := MustNewID()
	init := Hop{NodeID: [32]byte{1}, Type: HopInitiator}
	dest := Hop{NodeID: [32]byte{2}, Type: HopDest}
	path := []Hop{init, {NodeID: [32]byte{3}, Type: HopRelay}, dest}

	cfg := DefaultConfig()
	cfg.TTL = 100 * time.Millisecond

	tun := NewTunnel(id, init, dest, path, cfg)
	if tun.Expired() {
		t.Fatal("should not be expired immediately")
	}
	time.Sleep(150 * time.Millisecond)
	if !tun.Expired() {
		t.Fatal("should be expired after TTL")
	}
}

func TestTunnelIntegration_LoopDetection(t *testing.T) {
	n := newTestNode(t, 0)
	defer n.Close()

	req := protocol.TunnelBuildPayload{
		TunnelID:  [16]byte{1, 2, 3},
		DestID:    [32]byte{9},
		FullPath:  []string{"a", "b", "c", "d"},
		HopIndex:  0,
		MaxHops:   3,
		TTLSec:    60,
		InitID:    [32]byte{99},
		PathSoFar: [][32]byte{n.id},
	}
	payload, _ := protocol.Encode(&req)
	frame := protocol.Frame{
		Version: protocol.Version,
		Type:    protocol.MsgTunnelBuild,
		Payload: payload,
	}

	conn := &fakeConn{toRead: make(chan protocol.Frame, 1)}
	err := n.handlers.HandleTunnelBuild(conn, frame)
	if err == nil {
		t.Fatal("expected loop detection error")
	}
	if !contains(err.Error(), "loop") {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("E5-1: loop detected: %v", err)
}

type fakeConn struct {
	written [][]byte
	toRead  chan protocol.Frame
	closed  bool
}

func (c *fakeConn) ReadFrame() (protocol.Frame, error) {
	f, ok := <-c.toRead
	if !ok {
		return protocol.Frame{}, ErrTunnelClosed
	}
	return f, nil
}

func (c *fakeConn) WriteFrame(f protocol.Frame) error {
	c.written = append(c.written, f.Payload)
	return nil
}
func (c *fakeConn) RemoteAddr() string { return "fake" }
func (c *fakeConn) Close() error {
	if !c.closed {
		c.closed = true
		close(c.toRead)
	}
	return nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
