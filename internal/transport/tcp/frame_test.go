package tcp

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"p2pnode/internal/transport"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		f    transport.Frame
	}{
		{"empty", transport.Frame{Type: transport.MsgPing}},
		{"small", transport.Frame{Type: transport.MsgText, Payload: []byte("hello")}},
		{"1KB", transport.Frame{Type: transport.MsgText, Payload: bytes.Repeat([]byte{0xAB}, 1024)}},
		{"64KB", transport.Frame{Type: transport.MsgFileChunk, Payload: bytes.Repeat([]byte{0xCD}, 64*1024)}},
		{"1MB", transport.Frame{Type: transport.MsgFileChunk, Payload: bytes.Repeat([]byte{0xEF}, 1024*1024)}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, c.f); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}
			got, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if got.Type != c.f.Type {
				t.Fatalf("type: got %v want %v", got.Type, c.f.Type)
			}
			if !bytes.Equal(got.Payload, c.f.Payload) {
				t.Fatalf("payload mismatch: len got %d want %d", len(got.Payload), len(c.f.Payload))
			}
		})
	}
}

func TestReadFrame_BadMagic(t *testing.T) {
	buf := bytes.NewReader([]byte{0x00, 0x00, 0x01, 0, 0, 0, 0})
	_, err := ReadFrame(buf)
	if !errors.Is(err, ErrBadMagic) {
		t.Fatalf("want ErrBadMagic, got %v", err)
	}
}

func TestReadFrame_TooLarge(t *testing.T) {
	hdr := []byte{0x50, 0x32, 0x20, 0xFF, 0xFF, 0xFF, 0xFF}
	_, err := ReadFrame(bytes.NewReader(hdr))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestReadFrame_EOF(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestReadFrame_TruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteFrame(&buf, transport.Frame{Type: transport.MsgText, Payload: bytes.Repeat([]byte{1}, 100)})
	truncated := buf.Bytes()[:headerSize+50]
	_, err := ReadFrame(bytes.NewReader(truncated))
	if err == nil {
		t.Fatal("want error, got nil")
	}
}
