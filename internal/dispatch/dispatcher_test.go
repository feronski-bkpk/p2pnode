package dispatch

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"p2pnode/internal/transport"
)

type fakeConn struct {
	mu     sync.Mutex
	frames []transport.Frame
	read   int
	writes []transport.Frame
	closed bool
}

func (f *fakeConn) ReadFrame() (transport.Frame, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.read >= len(f.frames) {
		return transport.Frame{}, io.EOF
	}
	fr := f.frames[f.read]
	f.read++
	return fr, nil
}

func (f *fakeConn) WriteFrame(fr transport.Frame) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return transport.ErrClosed
	}
	f.writes = append(f.writes, fr)
	return nil
}

func (f *fakeConn) RemoteAddr() string { return "fake:0" }
func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRegisterAndDispatch(t *testing.T) {
	d := New(discardLogger())
	var got transport.Frame
	d.Register(transport.MsgText, func(c transport.Conn, f transport.Frame) error {
		got = f
		return nil
	})
	c := &fakeConn{}
	f := transport.Frame{Type: transport.MsgText, Payload: []byte("hi")}
	if err := d.Dispatch(c, f); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !bytes.Equal(got.Payload, f.Payload) {
		t.Fatal("payload mismatch")
	}
}

func TestDispatchUnknownType(t *testing.T) {
	d := New(discardLogger())
	if err := d.Dispatch(&fakeConn{}, transport.Frame{Type: transport.MsgPing}); err == nil {
		t.Fatal("want error for unregistered type")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	d := New(discardLogger())
	h := func(c transport.Conn, f transport.Frame) error { return nil }
	d.Register(transport.MsgPing, h)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want panic on duplicate register")
		}
	}()
	d.Register(transport.MsgPing, h)
}

func TestServeReadsUntilEOF(t *testing.T) {
	d := New(discardLogger())
	var count int
	d.Register(transport.MsgText, func(c transport.Conn, f transport.Frame) error {
		count++
		return nil
	})
	c := &fakeConn{frames: []transport.Frame{
		{Type: transport.MsgText}, {Type: transport.MsgText}, {Type: transport.MsgText},
	}}
	d.Serve(c)
	if count != 3 {
		t.Fatalf("handled %d, want 3", count)
	}
	if !c.closed {
		t.Fatal("conn not closed after Serve")
	}
}

func TestServeContinuesAfterHandlerError(t *testing.T) {
	d := New(discardLogger())
	var count int
	d.Register(transport.MsgText, func(c transport.Conn, f transport.Frame) error {
		count++
		return errors.New("boom")
	})
	c := &fakeConn{frames: []transport.Frame{
		{Type: transport.MsgText}, {Type: transport.MsgText},
	}}
	d.Serve(c)
	if count != 2 {
		t.Fatalf("handled %d, want 2", count)
	}
}
