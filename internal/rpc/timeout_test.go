package rpc

import (
	"context"
	"net"
	"testing"
	"time"

	"p2pnode/internal/transport/tcp"
)

func TestRPC_SilentPeer_HandshakeDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		c, _ := ln.Accept()
		if c != nil {
			time.Sleep(5 * time.Second)
			c.Close()
		}
	}()

	tr := tcp.New()
	cl := NewClient(tr, discardLog(), nil, testIdentity(t))
	local := makeLocalContact(t, "127.0.0.1", 1)

	start := time.Now()
	_, err = cl.Ping(context.Background(), local, ln.Addr().String(), nil, 500*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("want timeout error")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("elapsed %v exceeds deadline (want ~500ms)", elapsed)
	}
	if elapsed < 400*time.Millisecond {
		t.Fatalf("elapsed %v too short, deadline not enforced", elapsed)
	}
}
