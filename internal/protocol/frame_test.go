package protocol

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		f    Frame
	}{
		{"ping_empty", Frame{Version: Version, Type: MsgPing}},
		{"pong_small", Frame{Version: Version, Type: MsgPong, Payload: []byte("hi")}},
		{"find_node_1k", Frame{Version: Version, Type: MsgFindNodeRequest, Payload: bytes.Repeat([]byte{0xAB}, 1024)}},
		{"find_node_64k", Frame{Version: Version, Type: MsgFindNodeResponse, Payload: bytes.Repeat([]byte{0xCD}, 65536)}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := c.f.WriteTo(&buf); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			got, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if got.Version != c.f.Version {
				t.Fatalf("version: %d vs %d", got.Version, c.f.Version)
			}
			if got.Type != c.f.Type {
				t.Fatalf("type: %v vs %v", got.Type, c.f.Type)
			}
			if got.Flags != c.f.Flags {
				t.Fatalf("flags: %d vs %d", got.Flags, c.f.Flags)
			}
			if got.RequestID != c.f.RequestID {
				t.Fatalf("request_id mismatch")
			}
			if !bytes.Equal(got.Payload, c.f.Payload) {
				t.Fatalf("payload mismatch: len %d vs %d", len(got.Payload), len(c.f.Payload))
			}
		})
	}
}

func TestReadFrame_ShortHeader(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader([]byte{Version, 0x01, 0, 0, 1, 2, 3, 4, 5, 6}))
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrShortHeader) {
		t.Fatalf("want ErrShortHeader, got %v", err)
	}
}

func TestReadFrame_EOF(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestReadFrame_BadVersion(t *testing.T) {
	var hdr [HeaderSize]byte
	hdr[0] = 99
	hdr[1] = byte(MsgPing)
	_, err := ReadFrame(bytes.NewReader(hdr[:]))
	if !errors.Is(err, ErrBadVersion) {
		t.Fatalf("want ErrBadVersion, got %v", err)
	}
}

func TestReadFrame_BadType(t *testing.T) {
	var hdr [HeaderSize]byte
	hdr[0] = Version
	hdr[1] = 0x55
	_, err := ReadFrame(bytes.NewReader(hdr[:]))
	if !errors.Is(err, ErrBadType) {
		t.Fatalf("want ErrBadType, got %v", err)
	}
}

func TestReadFrame_PayloadTooBig(t *testing.T) {
	var hdr [HeaderSize]byte
	hdr[0] = Version
	hdr[1] = byte(MsgPing)
	hdr[20] = 0x00
	hdr[21] = 0x01
	hdr[22] = 0x00
	hdr[23] = 0x01
	_, err := ReadFrame(bytes.NewReader(hdr[:]))
	if !errors.Is(err, ErrPayloadTooBig) {
		t.Fatalf("want ErrPayloadTooBig, got %v", err)
	}
}

func TestWriteTo_PayloadTooBig(t *testing.T) {
	f := Frame{
		Version: Version,
		Type:    MsgPing,
		Payload: make([]byte, MaxFramePayload+1),
	}
	_, err := f.WriteTo(&bytes.Buffer{})
	if !errors.Is(err, ErrPayloadTooBig) {
		t.Fatalf("want ErrPayloadTooBig, got %v", err)
	}
}

func TestReadFrame_ShortPayload(t *testing.T) {
	f := Frame{Version: Version, Type: MsgPing, Payload: bytes.Repeat([]byte{1}, 100)}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	truncated := buf.Bytes()[:HeaderSize+50]
	_, err := ReadFrame(bytes.NewReader(truncated))
	if !errors.Is(err, ErrShortPayload) {
		t.Fatalf("want ErrShortPayload, got %v", err)
	}
}

func TestTwoFramesInOneStream(t *testing.T) {
	f1 := Frame{Version: Version, Type: MsgPing, Payload: []byte("a")}
	f2 := Frame{Version: Version, Type: MsgPong, Payload: []byte("b")}

	var buf bytes.Buffer
	_, _ = f1.WriteTo(&buf)
	_, _ = f2.WriteTo(&buf)

	got1, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read1: %v", err)
	}
	got2, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read2: %v", err)
	}
	if !bytes.Equal(got1.Payload, []byte("a")) || !bytes.Equal(got2.Payload, []byte("b")) {
		t.Fatal("payload mismatch")
	}
}

func TestRequestIDPreserved(t *testing.T) {
	var rid RequestID
	for i := range rid {
		rid[i] = byte(i)
	}
	f := Frame{Version: Version, Type: MsgPing, RequestID: rid}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if got.RequestID != rid {
		t.Fatal("request_id mismatch")
	}
}
