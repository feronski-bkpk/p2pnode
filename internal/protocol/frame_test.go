package protocol

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
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

type chunkedReader struct {
	data     []byte
	off      int
	maxChunk int
	rng      *rand.Rand
}

func newChunkedReader(data []byte, maxChunk int, rng *rand.Rand) *chunkedReader {
	return &chunkedReader{data: data, maxChunk: maxChunk, rng: rng}
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.off >= len(c.data) {
		return 0, io.EOF
	}
	n := len(p)
	if c.maxChunk > 0 {
		if n > c.maxChunk {
			n = c.maxChunk
		}
	} else if n > 1 {
		n = 1 + c.rng.Intn(n)
	}
	if n > len(c.data)-c.off {
		n = len(c.data) - c.off
	}
	copy(p, c.data[c.off:c.off+n])
	c.off += n
	return n, nil
}

func encodeFrame(t *testing.T, f Frame) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return buf.Bytes()
}

func TestReadFrame_RandomChunkBoundaries(t *testing.T) {
	f := Frame{
		Version:   Version,
		Type:      MsgFindNodeRequest,
		Flags:     0,
		RequestID: RequestID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Payload:   bytes.Repeat([]byte{0xAB}, 257),
	}
	raw := encodeFrame(t, f)

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 2000; i++ {
		r := newChunkedReader(raw, 0, rng)
		got, err := ReadFrame(r)
		if err != nil {
			t.Fatalf("iter %d: ReadFrame: %v", i, err)
		}
		if got.Version != f.Version || got.Type != f.Type || got.Flags != f.Flags {
			t.Fatalf("iter %d: header mismatch", i)
		}
		if got.RequestID != f.RequestID {
			t.Fatalf("iter %d: request_id mismatch", i)
		}
		if !bytes.Equal(got.Payload, f.Payload) {
			t.Fatalf("iter %d: payload mismatch", i)
		}
	}
}

func TestReadFrame_ChunkedSizes(t *testing.T) {
	cases := []struct {
		name     string
		payload  int
		maxChunk int
	}{
		{"1-byte chunks, empty payload", 0, 1},
		{"1-byte chunks, small payload", 5, 1},
		{"1-byte chunks, header-size payload", 24, 1},
		{"header boundary -1 (23)", 100, 23},
		{"header boundary exact (24)", 100, 24},
		{"header boundary +1 (25)", 100, 25},
		{"payload + header in one chunk", 100, 124},
		{"max payload chunked by 7", MaxFramePayload, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := Frame{
				Version:   Version,
				Type:      MsgPing,
				RequestID: RequestID{9},
				Payload:   bytes.Repeat([]byte{0xCD}, tc.payload),
			}
			raw := encodeFrame(t, f)
			r := newChunkedReader(raw, tc.maxChunk, nil)
			got, err := ReadFrame(r)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if got.Type != f.Type {
				t.Fatalf("type mismatch: %v vs %v", got.Type, f.Type)
			}
			if !bytes.Equal(got.Payload, f.Payload) {
				t.Fatalf("payload mismatch: len %d vs %d", len(got.Payload), len(f.Payload))
			}
			if _, err := ReadFrame(r); !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF after frame, got %v", err)
			}
		})
	}
}

func TestReadFrame_TwoFramesChunked(t *testing.T) {
	f1 := Frame{Version: Version, Type: MsgPing, RequestID: RequestID{1}, Payload: []byte("first")}
	f2 := Frame{Version: Version, Type: MsgPong, RequestID: RequestID{2}, Payload: []byte("second-frame")}
	raw := append(encodeFrame(t, f1), encodeFrame(t, f2)...)

	for _, maxChunk := range []int{1, 2, 7, 23, 24, 25, 50, 1000} {
		r := newChunkedReader(raw, maxChunk, nil)
		got1, err := ReadFrame(r)
		if err != nil {
			t.Fatalf("maxChunk=%d: first: %v", maxChunk, err)
		}
		got2, err := ReadFrame(r)
		if err != nil {
			t.Fatalf("maxChunk=%d: second: %v", maxChunk, err)
		}
		if !bytes.Equal(got1.Payload, f1.Payload) {
			t.Fatalf("maxChunk=%d: first payload mismatch", maxChunk)
		}
		if !bytes.Equal(got2.Payload, f2.Payload) {
			t.Fatalf("maxChunk=%d: second payload mismatch", maxChunk)
		}
		if _, err := ReadFrame(r); !errors.Is(err, io.EOF) {
			t.Fatalf("maxChunk=%d: expected EOF after two frames, got %v", maxChunk, err)
		}
	}
}

type twoChunkEOFReader struct {
	first, second []byte
	stage         int
}

func (r *twoChunkEOFReader) Read(p []byte) (int, error) {
	switch r.stage {
	case 0:
		n := copy(p, r.first)
		if n < len(r.first) {
			r.first = r.first[n:]
			return n, nil
		}
		r.stage = 1
		return n, nil
	case 1:
		n := copy(p, r.second)
		if n < len(r.second) {
			r.second = r.second[n:]
			return n, nil
		}
		r.stage = 2
		return n, io.EOF
	default:
		return 0, io.EOF
	}
}

func TestReadFrame_EOFWithData(t *testing.T) {
	f := Frame{Version: Version, Type: MsgPing, RequestID: RequestID{7}, Payload: []byte("x")}
	raw := encodeFrame(t, f)
	half := len(raw) / 2
	r := &twoChunkEOFReader{first: raw[:half], second: raw[half:]}
	got, err := ReadFrame(r)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got.Payload, f.Payload) {
		t.Fatalf("payload mismatch")
	}
}
