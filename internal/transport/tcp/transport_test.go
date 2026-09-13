package tcp

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"p2pnode/internal/transport"
)

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
		if f.Type != transport.MsgText || !bytes.Equal(f.Payload, []byte("hello from client")) {
			t.Errorf("server got unexpected frame: %v %q", f.Type, f.Payload)
			return
		}
		if err := c.WriteFrame(transport.Frame{
			Type:    transport.MsgText,
			Payload: []byte("hello from server"),
		}); err != nil {
			t.Errorf("server WriteFrame: %v", err)
		}
	}()

	c, err := tr.Dial(ln.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if err := c.WriteFrame(transport.Frame{
		Type:    transport.MsgText,
		Payload: []byte("hello from client"),
	}); err != nil {
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
		if err := c.WriteFrame(transport.Frame{Type: transport.MsgText, Payload: p}); err != nil {
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
	tr := New()
	_, err := tr.Dial("127.0.0.1:1")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if errors.Is(err, transport.ErrClosed) {
		t.Fatal("unexpected ErrClosed")
	}
	_ = time.Now
}
