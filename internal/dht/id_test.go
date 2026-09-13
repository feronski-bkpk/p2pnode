package dht

import (
	"bytes"
	"testing"
)

func TestDistanceSymmetric(t *testing.T) {
	a, _ := RandomID()
	b, _ := RandomID()
	if Distance(a, b) != Distance(b, a) {
		t.Fatal("XOR distance must be symmetric")
	}
}

func TestDistanceSelfZero(t *testing.T) {
	a, _ := RandomID()
	if !Distance(a, a).IsZero() {
		t.Fatal("distance to self must be zero")
	}
}

func TestTriangleInequality(t *testing.T) {
	a, _ := RandomID()
	b, _ := RandomID()
	c, _ := RandomID()
	dac := Distance(a, c)
	dab := Distance(a, b)
	dbc := Distance(b, c)
	for i := 0; i < IDLen; i++ {
		if dac[i] != (dab[i] ^ dbc[i]) {
			t.Fatalf("byte %d: d(a,c)=%08b, d(a,b)^d(b,c)=%08b", i, dac[i], dab[i]^dbc[i])
		}
	}
}

func TestCommonPrefixLen(t *testing.T) {
	var a, b ID
	if got := CommonPrefixLen(a, b); got != IDBits {
		t.Fatalf("equal ids: got %d, want %d", got, IDBits)
	}
	b[0] = 0x80
	if got := CommonPrefixLen(a, b); got != 0 {
		t.Fatalf("first bit differs: got %d, want 0", got)
	}
	b[0] = 0x40
	if got := CommonPrefixLen(a, b); got != 1 {
		t.Fatalf("second bit differs: got %d, want 1", got)
	}
	b[0] = 0x01
	if got := CommonPrefixLen(a, b); got != 7 {
		t.Fatalf("eighth bit differs: got %d, want 7", got)
	}
	var c ID
	c[31] = 0x01
	if got := CommonPrefixLen(a, c); got != IDBits-1 {
		t.Fatalf("last bit differs: got %d, want %d", got, IDBits-1)
	}
}

func TestCloserTo(t *testing.T) {
	var target ID
	target[0] = 0x10
	var a ID
	a[0] = 0x11
	var b ID
	b[0] = 0x20
	if !CloserTo(target, a, b) {
		t.Fatal("a must be closer to target than b")
	}
	if CloserTo(target, b, a) {
		t.Fatal("b must not be closer to target than a")
	}
}

func TestIDHexRoundTrip(t *testing.T) {
	a, _ := RandomID()
	b, err := IDFromHex(a.String())
	if err != nil {
		t.Fatalf("IDFromHex: %v", err)
	}
	if !bytes.Equal(a[:], b[:]) {
		t.Fatal("hex round trip mismatch")
	}
}
