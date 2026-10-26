package tcp

import (
	"errors"
	"net"
	"testing"
	"time"

	"p2pnode/internal/protocol"
	"p2pnode/internal/transport"
)

func TestConn_WriteDeadline_PeerNotReading(t *testing.T) {
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

	tr := New()
	c, err := tr.Dial(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if tcpConn, ok := c.(*conn); ok {
		if raw, ok := tcpConn.nc.(*net.TCPConn); ok {
			_ = raw.SetWriteBuffer(4096)
		}
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	if err := c.SetWriteDeadline(deadline); err != nil {
		t.Fatal(err)
	}

	payload := make([]byte, 64*1024)
	frame := protocol.Frame{Version: protocol.Version, Type: protocol.MsgPing, Payload: payload}

	start := time.Now()
	var lastErr error
	for i := 0; i < 2000; i++ {
		if err := c.WriteFrame(frame); err != nil {
			lastErr = err
			break
		}
	}
	elapsed := time.Since(start)

	if lastErr == nil {
		t.Fatal("write never blocked — increase payload or reduce SNDBUF")
	}
	if !errors.Is(lastErr, transport.ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", lastErr)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("write deadline not enforced: %v", elapsed)
	}
}

func TestConn_ReadDeadline_SilentPeer(t *testing.T) {
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

	tr := New()
	c, err := tr.Dial(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	start := time.Now()
	if err := c.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = c.ReadFrame()
	elapsed := time.Since(start)

	if !errors.Is(err, transport.ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("read deadline not enforced: %v", elapsed)
	}
}
