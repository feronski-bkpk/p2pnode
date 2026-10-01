package rpc

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"p2pnode/internal/identity"
	"p2pnode/internal/routing"
	"p2pnode/internal/store"
	"p2pnode/internal/transport"
	"p2pnode/internal/transport/tcp"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testIdentity(t *testing.T) *identity.Identity {
	t.Helper()
	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}
	return id
}

func makeLocalContact(t *testing.T, host string, port uint16) routing.Contact {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return routing.Contact{
		NodeID:            identity.NodeIDFromPublicKey(pub),
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pub,
		Host:              host,
		Port:              port,
	}
}

type testNode struct {
	contact routing.Contact
	table   *routing.RoutingTable
	ln      transport.Listener
	server  *Server
	id      *identity.Identity
}

func newTestNetwork(t *testing.T, n, k int) []*testNode {
	t.Helper()
	tr := tcp.New()
	log := discardLog()
	nodes := make([]*testNode, 0, n)

	for i := 0; i < n; i++ {
		ln, err := tr.Listen("127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		host, portStr, err := net.SplitHostPort(ln.Addr())
		if err != nil {
			t.Fatalf("split: %v", err)
		}
		p, _ := strconv.Atoi(portStr)

		id := testIdentity(t)
		c := routing.Contact{
			NodeID:            id.NodeID,
			IdentityAlgorithm: "ed25519",
			IdentityPublicKey: id.PublicKey,
			Host:              host,
			Port:              uint16(p),
		}
		table := routing.NewRoutingTable(c.NodeID, k)
		st := store.New()
		srv := NewServer(c, id, table, st, nil, log, nil)

		tn := &testNode{contact: c, table: table, ln: ln, server: srv, id: id}
		nodes = append(nodes, tn)

		go srv.Serve(ln)
	}
	t.Cleanup(func() {
		for _, tn := range nodes {
			tn.ln.Close()
		}
	})
	return nodes
}

func TestPingRoundTrip(t *testing.T) {
	nodes := newTestNetwork(t, 2, 4)
	tr := tcp.New()
	client := NewClient(tr, discardLog(), nil, testIdentity(t))

	expected := nodes[1].contact.NodeID
	responder, err := client.Ping(nodes[0].contact, nodes[1].contact.Addr(), &expected, 2*time.Second)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if responder.NodeID != nodes[1].contact.NodeID {
		t.Fatal("wrong responder")
	}
}

func TestPingTimeout(t *testing.T) {
	nodes := newTestNetwork(t, 1, 4)
	tr := tcp.New()
	client := NewClient(tr, discardLog(), nil, testIdentity(t))

	_, err := client.Ping(nodes[0].contact, "127.0.0.1:1", nil, 300*time.Millisecond)
	if err == nil {
		t.Fatal("Ping to dead address should fail")
	}
}

func TestPingWithoutExpectedID(t *testing.T) {
	nodes := newTestNetwork(t, 2, 4)
	tr := tcp.New()
	client := NewClient(tr, discardLog(), nil, testIdentity(t))

	responder, err := client.Ping(nodes[0].contact, nodes[1].contact.Addr(), nil, 2*time.Second)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if responder.NodeID != nodes[1].contact.NodeID {
		t.Fatal("wrong responder")
	}
}

func TestRequestIDMismatch(t *testing.T) {
	t.Skip("requires custom handshake manipulation; covered by crypto tests")
	_ = bytes.Contains
}
