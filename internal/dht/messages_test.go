package dht

import (
	"bytes"
	"testing"
	"time"
)

func TestPingRoundTrip(t *testing.T) {
	req := PingRequest{
		FromID:   IDFromBytes([]byte("alice")),
		FromAddr: "127.0.0.1:9001",
	}
	b, err := Encode(req)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got PingRequest
	if err := Decode(b, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.FromID != req.FromID || got.FromAddr != req.FromAddr {
		t.Fatalf("mismatch: %+v vs %+v", got, req)
	}
}

func TestFindNodeResponseRoundTrip(t *testing.T) {
	resp := FindNodeResponse{
		FromID: IDFromBytes([]byte("seed")),
		Nodes: []Node{
			{ID: IDFromBytes([]byte("a")), Addr: "127.0.0.1:9001", LastSeen: time.Now().Truncate(time.Millisecond)},
			{ID: IDFromBytes([]byte("b")), Addr: "127.0.0.1:9002", LastSeen: time.Now().Truncate(time.Millisecond)},
		},
	}
	b, err := Encode(resp)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got FindNodeResponse
	if err := Decode(b, &got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.FromID != resp.FromID {
		t.Fatal("FromID mismatch")
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("nodes: got %d, want 2", len(got.Nodes))
	}
	for i := range got.Nodes {
		if got.Nodes[i].ID != resp.Nodes[i].ID {
			t.Fatalf("node %d: id mismatch", i)
		}
		if got.Nodes[i].Addr != resp.Nodes[i].Addr {
			t.Fatalf("node %d: addr mismatch", i)
		}
		if !got.Nodes[i].LastSeen.Equal(resp.Nodes[i].LastSeen) {
			t.Fatalf("node %d: time mismatch: %v vs %v", i, got.Nodes[i].LastSeen, resp.Nodes[i].LastSeen)
		}
	}
}

func TestDecodeGarbage(t *testing.T) {
	var got PingRequest
	err := Decode([]byte{0xFF, 0xFF, 0xFF}, &got)
	if err == nil {
		t.Fatal("want error on garbage")
	}
	_ = bytes.Equal
}
