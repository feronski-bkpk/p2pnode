package routing

import (
	"crypto/rand"
	"testing"
)

func randomID(t *testing.T) ID {
	t.Helper()
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return id
}

func TestDistanceSymmetric(t *testing.T) {
	a := randomID(t)
	b := randomID(t)
	if Distance(a, b) != Distance(b, a) {
		t.Fatal("XOR distance must be symmetric")
	}
}

func TestDistanceSelfZero(t *testing.T) {
	a := randomID(t)
	if !Distance(a, a).IsZero() {
		t.Fatal("distance to self must be zero")
	}
}

func TestTriangleIdentity(t *testing.T) {
	a := randomID(t)
	b := randomID(t)
	c := randomID(t)
	dac := Distance(a, c)
	dab := Distance(a, b)
	dbc := Distance(b, c)
	for i := range dac {
		if dac[i] != (dab[i] ^ dbc[i]) {
			t.Fatalf("byte %d: XOR identity violated", i)
		}
	}
}

func TestCloserTo(t *testing.T) {
	var target, a, b ID
	target[0] = 0x10
	a[0] = 0x11
	b[0] = 0x20
	if !CloserTo(target, a, b) {
		t.Fatal("a must be closer to target than b")
	}
	if CloserTo(target, b, a) {
		t.Fatal("b must not be closer to target than a")
	}
}

func TestCommonPrefixLen(t *testing.T) {
	var a, b ID
	if got := CommonPrefixLen(a, b); got != IDBits {
		t.Fatalf("equal ids: got %d, want %d", got, IDBits)
	}
	b[0] = 0x80
	if got := CommonPrefixLen(a, b); got != 0 {
		t.Fatalf("first bit: got %d, want 0", got)
	}
	b[0] = 0x40
	if got := CommonPrefixLen(a, b); got != 1 {
		t.Fatalf("second bit: got %d, want 1", got)
	}
}

func TestCompare(t *testing.T) {
	var a, b ID
	a[0] = 1
	b[0] = 2
	if Compare(a, b) != -1 {
		t.Fatal("a < b")
	}
	if Compare(b, a) != 1 {
		t.Fatal("b > a")
	}
	if Compare(a, a) != 0 {
		t.Fatal("a == a")
	}
}
