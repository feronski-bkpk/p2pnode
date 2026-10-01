package integration

import (
	"testing"
	"time"

	"p2pnode/internal/node"
)

func startCluster(t *testing.T, n int, basePort int) []*node.Node {
	t.Helper()
	if n < 1 {
		t.Fatal("startCluster: n must be >= 1")
	}

	seed := startNode(t, t.TempDir(), basePort, nil)
	t.Cleanup(func() { _ = seed.Stop() })
	time.Sleep(30 * time.Millisecond)

	nodes := []*node.Node{seed}

	for i := 1; i < n; i++ {
		dir := t.TempDir()
		port := basePort + i
		n := startNode(t, dir, port, []string{addr(basePort)})
		t.Cleanup(func() { _ = n.Stop() })
		nodes = append(nodes, n)
	}

	for i := 1; i < n; i++ {
		if err := nodes[i].Bootstrap(); err != nil {
			t.Fatalf("bootstrap node %d: %v", i, err)
		}
	}

	time.Sleep(500 * time.Millisecond)

	return nodes
}
