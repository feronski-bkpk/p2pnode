package integration

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"p2pnode/internal/config"
	"p2pnode/internal/node"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func startNode(t *testing.T, stateDir string, port int, bootstrap []string) *node.Node {
	t.Helper()
	cfg := config.Default()
	cfg.NodeStateDir = stateDir
	cfg.ListenHost = "127.0.0.1"
	cfg.ListenPort = port
	cfg.BootstrapPeers = bootstrap
	cfg.KBucketSize = 4
	cfg.Alpha = 3

	n, err := node.New(cfg, discardLog())
	if err != nil {
		t.Fatalf("node.New: %v", err)
	}
	n.Start()
	return n
}

func TestStar5Nodes(t *testing.T) {
	seed := startNode(t, t.TempDir(), 19001, nil)
	defer seed.Stop()
	time.Sleep(50 * time.Millisecond)

	nodes := []*node.Node{seed}
	for i := 0; i < 4; i++ {
		dir := t.TempDir()
		port := 19002 + i
		n := startNode(t, dir, port, []string{"127.0.0.1:19001"})
		defer n.Stop()
		nodes = append(nodes, n)
	}

	for i := 1; i < len(nodes); i++ {
		if err := nodes[i].Bootstrap(); err != nil {
			t.Fatalf("node %d bootstrap: %v", i, err)
		}
	}

	time.Sleep(300 * time.Millisecond)

	for i, n := range nodes {
		size := n.Table.Size()
		if size < 1 {
			t.Errorf("node %d knows %d peers", i, size)
		}
	}
}

func TestRing5Nodes(t *testing.T) {
	seed := startNode(t, t.TempDir(), 19101, nil)
	defer seed.Stop()
	time.Sleep(50 * time.Millisecond)

	nodes := []*node.Node{seed}
	ports := []int{19101}
	for i := 1; i < 5; i++ {
		dir := t.TempDir()
		port := 19101 + i
		prevPort := ports[i-1]
		bootstrap := []string{addr(prevPort)}
		n := startNode(t, dir, port, bootstrap)
		defer n.Stop()
		nodes = append(nodes, n)
		ports = append(ports, port)

		if err := n.Bootstrap(); err != nil {
			t.Fatalf("node %d bootstrap: %v", i, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(300 * time.Millisecond)

	for i, n := range nodes {
		size := n.Table.Size()
		if size < 1 {
			t.Errorf("node %d knows %d peers", i, size)
		}
	}
}

func addr(port int) string {
	return "127.0.0.1:" + itoa(port)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
