package crypto

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"

	"p2pnode/internal/protocol"
	"p2pnode/internal/transport"
)

type pipeConn struct {
	mu     sync.Mutex
	inbox  []protocol.Frame
	read   int
	writes []protocol.Frame
	closed bool
	peer   *pipeConn
}

func newPipePair() (*pipeConn, *pipeConn) {
	a := &pipeConn{}
	b := &pipeConn{}
	a.peer = b
	b.peer = a
	return a, b
}

func (p *pipeConn) ReadFrame() (protocol.Frame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.read >= len(p.inbox) {
		return protocol.Frame{}, io.EOF
	}
	f := p.inbox[p.read]
	p.read++
	return f, nil
}

func (p *pipeConn) WriteFrame(f protocol.Frame) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return transport.ErrClosed
	}
	p.peer.mu.Lock()
	p.peer.inbox = append(p.peer.inbox, f)
	p.peer.mu.Unlock()
	return nil
}

func (p *pipeConn) RemoteAddr() string { return "pipe" }
func (p *pipeConn) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func TestSecureConn_RoundTrip(t *testing.T) {
	a, b := newPipePair()

	keyAB := MustRandomBytes(32)
	keyBA := MustRandomBytes(32)

	var sid SessionID
	copy(sid[:], MustRandomBytes(16))

	secA, err := NewSecureConn(a, keyAB, keyBA, sid)
	if err != nil {
		t.Fatalf("NewSecureConn A: %v", err)
	}
	secB, err := NewSecureConn(b, keyBA, keyAB, sid)
	if err != nil {
		t.Fatalf("NewSecureConn B: %v", err)
	}

	payload := []byte("hello, secure world")
	f := protocol.Frame{
		Version:   1,
		Type:      protocol.MsgPing,
		RequestID: [16]byte{1, 2, 3},
		Payload:   payload,
	}
	if err := secA.WriteFrame(f); err != nil {
		t.Fatalf("WriteFrame A: %v", err)
	}
	got, err := secB.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame B: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Fatalf("payload mismatch: %q vs %q", got.Payload, payload)
	}
	if got.Type != f.Type {
		t.Fatalf("type mismatch: %v vs %v", got.Type, f.Type)
	}
}

func TestSecureConn_ReplayRejected(t *testing.T) {
	a, b := newPipePair()
	keyAB := MustRandomBytes(32)
	keyBA := MustRandomBytes(32)
	var sid SessionID
	copy(sid[:], MustRandomBytes(16))

	secA, _ := NewSecureConn(a, keyAB, keyBA, sid)
	secB, _ := NewSecureConn(b, keyBA, keyAB, sid)

	f := protocol.Frame{
		Version:   1,
		Type:      protocol.MsgPing,
		RequestID: [16]byte{1},
		Payload:   []byte("replay me"),
	}
	if err := secA.WriteFrame(f); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	b.mu.Lock()
	raw := b.inbox[len(b.inbox)-1]
	b.mu.Unlock()

	_, err := secB.ReadFrame()
	if err != nil {
		t.Fatalf("first ReadFrame: %v", err)
	}

	b.mu.Lock()
	b.inbox = append(b.inbox, raw)
	b.mu.Unlock()

	_, err = secB.ReadFrame()
	if !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("want ErrReplayDetected, got %v", err)
	}
}

func TestSecureConn_TamperedPayload(t *testing.T) {
	a, b := newPipePair()
	keyAB := MustRandomBytes(32)
	keyBA := MustRandomBytes(32)
	var sid SessionID
	copy(sid[:], MustRandomBytes(16))

	secA, _ := NewSecureConn(a, keyAB, keyBA, sid)
	secB, _ := NewSecureConn(b, keyBA, keyAB, sid)

	f := protocol.Frame{
		Version:   1,
		Type:      protocol.MsgPing,
		RequestID: [16]byte{1},
		Payload:   []byte("original"),
	}
	_ = secA.WriteFrame(f)

	b.mu.Lock()
	raw := b.inbox[len(b.inbox)-1]
	raw.Payload[len(raw.Payload)-1] ^= 0xFF
	b.inbox[len(b.inbox)-1] = raw
	b.mu.Unlock()

	_, err := secB.ReadFrame()
	if err == nil {
		t.Fatal("want error for tampered payload")
	}
}

func TestSecureConn_TamperedHeader(t *testing.T) {
	a, b := newPipePair()
	keyAB := MustRandomBytes(32)
	keyBA := MustRandomBytes(32)
	var sid SessionID
	copy(sid[:], MustRandomBytes(16))

	secA, _ := NewSecureConn(a, keyAB, keyBA, sid)
	secB, _ := NewSecureConn(b, keyBA, keyAB, sid)

	f := protocol.Frame{
		Version:   1,
		Type:      protocol.MsgPing,
		RequestID: [16]byte{1},
		Payload:   []byte("data"),
	}
	_ = secA.WriteFrame(f)

	b.mu.Lock()
	raw := b.inbox[len(b.inbox)-1]
	raw.Type = protocol.MsgFindNodeRequest
	b.inbox[len(b.inbox)-1] = raw
	b.mu.Unlock()

	_, err := secB.ReadFrame()
	if err == nil {
		t.Fatal("want error for tampered header (AAD)")
	}
}

func TestSecureConn_WrongSessionID(t *testing.T) {
	a, b := newPipePair()
	keyAB := MustRandomBytes(32)
	keyBA := MustRandomBytes(32)
	var sidA, sidB SessionID
	copy(sidA[:], MustRandomBytes(16))
	copy(sidB[:], MustRandomBytes(16))

	secA, _ := NewSecureConn(a, keyAB, keyBA, sidA)
	secB, _ := NewSecureConn(b, keyBA, keyAB, sidB)

	_ = secA.WriteFrame(protocol.Frame{
		Version: 1,
		Type:    protocol.MsgPing,
		Payload: []byte("x"),
	})

	_, err := secB.ReadFrame()
	if !errors.Is(err, ErrSessionMismatch) {
		t.Fatalf("want ErrSessionMismatch, got %v", err)
	}
}

func TestSecureConn_ClosedRejects(t *testing.T) {
	a, _ := newPipePair()
	keyAB := MustRandomBytes(32)
	keyBA := MustRandomBytes(32)
	var sid SessionID
	copy(sid[:], MustRandomBytes(16))

	secA, _ := NewSecureConn(a, keyAB, keyBA, sid)
	_ = secA.Close()

	err := secA.WriteFrame(protocol.Frame{
		Version: 1,
		Type:    protocol.MsgPing,
		Payload: []byte("x"),
	})
	if !errors.Is(err, transport.ErrClosed) {
		t.Fatalf("want ErrClosed, got %v", err)
	}
}
