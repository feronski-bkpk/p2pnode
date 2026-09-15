package tcp

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"p2pnode/internal/protocol"
	"p2pnode/internal/transport"
)

func newTestFrame(t protocol.MsgType, payload []byte) protocol.Frame {
	var rid protocol.RequestID
	for i := range rid {
		rid[i] = byte(i)
	}
	return protocol.Frame{
		Version:   protocol.Version,
		Type:      t,
		RequestID: rid,
		Payload:   payload,
	}
}

func TestTwoNodesExchange(t *testing.T) {
	tr := New()

	ln, err := tr.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		c, err := ln.Accept()
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer c.Close()

		f, err := c.ReadFrame()
		if err != nil {
			t.Errorf("server ReadFrame: %v", err)
			return
		}
		if f.Type != protocol.MsgPing || !bytes.Equal(f.Payload, []byte("hello from client")) {
			t.Errorf("server got unexpected frame: %v %q", f.Type, f.Payload)
			return
		}
		if err := c.WriteFrame(newTestFrame(protocol.MsgPong, []byte("hello from server"))); err != nil {
			t.Errorf("server WriteFrame: %v", err)
		}
	}()

	c, err := tr.Dial(ln.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if err := c.WriteFrame(newTestFrame(protocol.MsgPing, []byte("hello from client"))); err != nil {
		t.Fatalf("client WriteFrame: %v", err)
	}

	got, err := c.ReadFrame()
	if err != nil {
		t.Fatalf("client ReadFrame: %v", err)
	}
	if !bytes.Equal(got.Payload, []byte("hello from server")) {
		t.Fatalf("client got %q", got.Payload)
	}

	wg.Wait()
}

func TestManyFrames(t *testing.T) {
	tr := New()
	ln, _ := tr.Listen("127.0.0.1:0")
	defer ln.Close()

	go func() {
		c, _ := ln.Accept()
		defer c.Close()
		for i := 0; i < 1000; i++ {
			f, err := c.ReadFrame()
			if err != nil {
				return
			}
			_ = c.WriteFrame(f)
		}
	}()

	c, _ := tr.Dial(ln.Addr())
	defer c.Close()

	payloads := [][]byte{
		{},
		[]byte("a"),
		bytes.Repeat([]byte{0xAA}, 1024),
		bytes.Repeat([]byte{0xBB}, 64*1024),
	}
	for i := 0; i < 1000; i++ {
		p := payloads[i%len(payloads)]
		if err := c.WriteFrame(newTestFrame(protocol.MsgFindNodeRequest, p)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		got, err := c.ReadFrame()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if !bytes.Equal(got.Payload, p) {
			t.Fatalf("mismatch at %d", i)
		}
	}
}

func TestDialTimeout(t *testing.T) {
	tr := NewWithOptions(Options{
		ConnectTimeout: 500 * time.Millisecond,
		ReadTimeout:    5 * time.Second,
	})
	_, err := tr.Dial("127.0.0.1:1")
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestReadTimeout(t *testing.T) {
	tr := NewWithOptions(Options{
		ConnectTimeout: 1 * time.Second,
		ReadTimeout:    200 * time.Millisecond,
	})

	ln, _ := tr.Listen("127.0.0.1:0")
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c
		time.Sleep(2 * time.Second)
	}()

	c, err := tr.Dial(ln.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, err = c.ReadFrame()
	if err == nil {
		t.Fatal("want read error, got nil")
	}
	if !errors.Is(err, transport.ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}

func TestConnectionClosedByPeer(t *testing.T) {
	tr := New()
	ln, _ := tr.Listen("127.0.0.1:0")
	defer ln.Close()

	go func() {
		c, _ := ln.Accept()
		if c != nil {
			c.Close()
		}
	}()

	c, err := tr.Dial(ln.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	time.Sleep(50 * time.Millisecond)
	_, err = c.ReadFrame()
	if err == nil {
		t.Fatal("want error, got nil")
	}
}
