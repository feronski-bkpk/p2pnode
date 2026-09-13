package dht

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"p2pnode/internal/transport"
	"p2pnode/internal/transport/tcp"
)

type testNode struct {
	dht *DHT
	ln  transport.Listener
}

func newTestCluster(t *testing.T, n int) []*testNode {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tr := tcp.New()

	out := make([]*testNode, 0, n)
	for i := 0; i < n; i++ {
		ln, err := tr.Listen("127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		id, err := RandomID()
		if err != nil {
			t.Fatalf("random id: %v", err)
		}
		d := New(id, ln.Addr(), tr, log)
		tn := &testNode{dht: d, ln: ln}
		out = append(out, tn)

		go func(tn *testNode) {
			for {
				conn, err := tn.ln.Accept()
				if err != nil {
					return
				}
				go serveConn(tn.dht, conn)
			}
		}(tn)
	}

	t.Cleanup(func() {
		for _, tn := range out {
			_ = tn.ln.Close()
		}
	})
	return out
}

func serveConn(d *DHT, c transport.Conn) {
	defer c.Close()
	for {
		f, err := c.ReadFrame()
		if err != nil {
			return
		}
		switch f.Type {
		case transport.MsgPing:
			_ = d.HandlePing(c, f)
		case transport.MsgFindNode:
			_ = d.HandleFindNode(c, f)
		default:
			return
		}
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestBootstrap3Nodes(t *testing.T) {
	nodes := newTestCluster(t, 3)
	seed := nodes[0]
	for i := 1; i < 3; i++ {
		if err := nodes[i].dht.Bootstrap(seed.ln.Addr()); err != nil {
			t.Fatalf("bootstrap node %d: %v", i, err)
		}
	}

	ok := waitFor(t, 3*time.Second, func() bool {
		for i, a := range nodes {
			for j, b := range nodes {
				if i == j {
					continue
				}
				if _, found := a.dht.table.Get(b.dht.SelfID); !found {
					return false
				}
			}
		}
		return true
	})
	if !ok {
		for i, a := range nodes {
			t.Logf("node %d (id=%s) knows:", i, a.dht.SelfID.String()[:8])
			for _, n := range a.dht.table.Snapshot() {
				t.Logf("  %s @ %s", n.ID.String()[:8], n.Addr)
			}
		}
		t.Fatal("not all nodes converged within timeout")
	}
}

func TestBootstrap5Nodes(t *testing.T) {
	nodes := newTestCluster(t, 5)
	seed := nodes[0]
	for i := 1; i < 5; i++ {
		if err := nodes[i].dht.Bootstrap(seed.ln.Addr()); err != nil {
			t.Fatalf("bootstrap node %d: %v", i, err)
		}
	}

	ok := waitFor(t, 5*time.Second, func() bool {
		for _, tn := range nodes {
			if tn.dht.table.Size() < 3 {
				return false
			}
		}
		return true
	})
	if !ok {
		for i, tn := range nodes {
			t.Logf("node %d knows %d peers", i, tn.dht.table.Size())
		}
		t.Fatal("not all nodes have >= 3 peers within timeout")
	}

	target := nodes[2].dht.SelfID
	found := nodes[4].dht.FindNode(target)
	if len(found) == 0 {
		t.Fatal("FindNode returned nothing")
	}
	hasTarget := false
	for _, n := range found {
		if n.ID == target {
			hasTarget = true
			break
		}
	}
	if !hasTarget {
		t.Errorf("target %s not in FindNode result", target.String()[:8])
	}
}

func TestPingDeadNode(t *testing.T) {
	nodes := newTestCluster(t, 2)
	_ = nodes[1].ln.Close()
	time.Sleep(50 * time.Millisecond)

	_, err := nodes[0].dht.Ping(Node{Addr: nodes[1].ln.Addr()})
	if err == nil {
		t.Fatal("Ping to dead node should fail")
	}
}

func TestUniqueSorted(t *testing.T) {
	var target ID
	target[0] = 0xFF
	nodes := []Node{
		{ID: ID{0x00}, Addr: "a"},
		{ID: ID{0x0F}, Addr: "b"},
		{ID: ID{0x00}, Addr: "a-dup"},
		{ID: ID{0x07}, Addr: "c"},
	}
	out := uniqueSorted(nodes, target, 10)
	if len(out) != 3 {
		t.Fatalf("got %d, want 3 (dup removed)", len(out))
	}
	want := []byte{0x0F, 0x07, 0x00}
	for i, n := range out {
		if n.ID[0] != want[i] {
			t.Fatalf("out[%d]: got %02x, want %02x", i, n.ID[0], want[i])
		}
	}
	_ = fmt.Sprintf
}
