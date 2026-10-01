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
	"p2pnode/internal/protocol"
	"p2pnode/internal/routing"
	"p2pnode/internal/transport"
	"p2pnode/internal/transport/tcp"
)

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

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type testNode struct {
	contact routing.Contact
	table   *routing.RoutingTable
	ln      transport.Listener
	server  *Server
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

		c := makeLocalContact(t, host, uint16(p))
		table := routing.NewRoutingTable(c.NodeID, k)
		srv := NewServer(c, table, nil, nil, log, nil)

		tn := &testNode{contact: c, table: table, ln: ln, server: srv}
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
	client := NewClient(tr, discardLog(), nil)

	expected := nodes[1].contact.NodeID
	responder, err := client.Ping(nodes[0].contact, nodes[1].contact.Addr(), &expected, 2*time.Second)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if responder.NodeID != nodes[1].contact.NodeID {
		t.Fatal("wrong responder")
	}
}

func TestPingWithoutExpectedID(t *testing.T) {
	nodes := newTestNetwork(t, 2, 4)
	tr := tcp.New()
	client := NewClient(tr, discardLog(), nil)

	responder, err := client.Ping(nodes[0].contact, nodes[1].contact.Addr(), nil, 2*time.Second)
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
	client := NewClient(tr, discardLog(), nil)

	_, err := client.Ping(nodes[0].contact, "127.0.0.1:1", nil, 300*time.Millisecond)
	if err == nil {
		t.Fatal("Ping to dead address should fail")
	}
}

func TestRequestIDMismatch(t *testing.T) {
	tr := tcp.New()
	ln, _ := tr.Listen("127.0.0.1:0")
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.ReadFrame()
		var wrongID protocol.RequestID
		wrongID[0] = 0xFF
		payload, _ := protocol.Encode(protocol.PongPayload{
			Responder: protocol.Contact{
				NodeID:            [32]byte{},
				IdentityAlgorithm: "ed25519",
				IdentityPublicKey: make([]byte, ed25519.PublicKeySize),
				Host:              "127.0.0.1",
				Port:              1,
			},
		})
		_ = conn.WriteFrame(protocol.Frame{
			Version:   protocol.Version,
			Type:      protocol.MsgPong,
			RequestID: wrongID,
			Payload:   payload,
		})
	}()

	client := NewClient(tr, discardLog(), nil)
	host, portStr, _ := net.SplitHostPort(ln.Addr())
	p, _ := strconv.Atoi(portStr)
	target := makeLocalContact(t, host, uint16(p))

	_, err := client.Ping(makeLocalContact(t, "127.0.0.1", 9999), target.Addr(), nil, 2*time.Second)
	if err == nil {
		t.Fatal("want error for mismatched request_id")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("mismatch")) {
		t.Fatalf("want mismatch, got %v", err)
	}
}
