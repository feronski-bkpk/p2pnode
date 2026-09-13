package dht

import (
	"fmt"
	"testing"
	"time"
)

func TestBucketIndex(t *testing.T) {
	var self ID
	var a ID
	a[0] = 0x80
	if got := bucketIndex(self, a); got != 0 {
		t.Fatalf("a: got %d, want 0", got)
	}
	var b ID
	b[0] = 0x40
	if got := bucketIndex(self, b); got != 1 {
		t.Fatalf("b: got %d, want 1", got)
	}
	var c ID
	c[31] = 0x01
	if got := bucketIndex(self, c); got != 255 {
		t.Fatalf("c: got %d, want 255", got)
	}
	if got := bucketIndex(self, self); got != -1 {
		t.Fatalf("self: got %d, want -1", got)
	}
}

func TestAddAndGet(t *testing.T) {
	self, _ := RandomID()
	rt := NewRoutingTable(self)

	for i := byte(1); i <= 5; i++ {
		n := Node{
			ID:   IDFromBytes([]byte{i}),
			Addr: fmt.Sprintf("127.0.0.1:%d", 9000+int(i)),
		}
		if !rt.Add(n) {
			t.Fatalf("Add %d failed", i)
		}
	}
	if rt.Size() != 5 {
		t.Fatalf("size: got %d, want 5", rt.Size())
	}

	n := Node{ID: IDFromBytes([]byte{3})}
	got, ok := rt.Get(n.ID)
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.Addr != "127.0.0.1:9003" {
		t.Fatalf("addr: got %s", got.Addr)
	}
}

func TestAddDuplicateUpdates(t *testing.T) {
	self, _ := RandomID()
	rt := NewRoutingTable(self)

	id := IDFromBytes([]byte("dup"))
	n1 := Node{ID: id, Addr: "127.0.0.1:9001", LastSeen: time.Now().Add(-time.Hour)}
	n2 := Node{ID: id, Addr: "127.0.0.1:9002", LastSeen: time.Now()}

	rt.Add(n1)
	if !rt.Add(n2) {
		t.Fatal("second Add failed")
	}
	if rt.Size() != 1 {
		t.Fatalf("size: got %d, want 1", rt.Size())
	}
	got, _ := rt.Get(id)
	if got.Addr != "127.0.0.1:9002" {
		t.Fatalf("addr should be updated: got %s", got.Addr)
	}
	if time.Since(got.LastSeen) > time.Second {
		t.Fatalf("LastSeen should be fresh")
	}
}

func TestBucketEviction(t *testing.T) {
	var s ID
	s[0] = 0x01
	rt := NewRoutingTable(s)

	var ids []ID
	for i := 0; i < K+2; i++ {
		var id ID
		id[0] = 0x80
		id[31] = byte(i + 1)
		ids = append(ids, id)
	}

	for i, id := range ids {
		rt.Add(Node{ID: id, Addr: fmt.Sprintf("127.0.0.1:%d", 10000+i)})
	}

	if rt.Size() != K {
		t.Fatalf("size: got %d, want %d (bucket full)", rt.Size(), K)
	}
	if _, ok := rt.Get(ids[0]); ok {
		t.Fatal("ids[0] should have been evicted")
	}
	if _, ok := rt.Get(ids[1]); ok {
		t.Fatal("ids[1] should have been evicted")
	}
	if _, ok := rt.Get(ids[K+1]); !ok {
		t.Fatal("last inserted node should be present")
	}
}

func TestClosest(t *testing.T) {
	self, _ := RandomID()
	rt := NewRoutingTable(self)

	var target ID
	target[0] = 0xFF

	for i := byte(0); i < 10; i++ {
		var id ID
		id[0] = i
		rt.Add(Node{ID: id, Addr: fmt.Sprintf("n%d", i)})
	}

	closest := rt.Closest(target, 3)
	if len(closest) != 3 {
		t.Fatalf("got %d, want 3", len(closest))
	}
	want := []byte{0x09, 0x08, 0x07}
	for i, n := range closest {
		if n.ID[0] != want[i] {
			t.Fatalf("closest[%d]: got %02x, want %02x", i, n.ID[0], want[i])
		}
	}
}

func TestRemove(t *testing.T) {
	self, _ := RandomID()
	rt := NewRoutingTable(self)
	id := IDFromBytes([]byte("x"))
	rt.Add(Node{ID: id, Addr: "addr"})
	if !rt.Remove(id) {
		t.Fatal("Remove returned false")
	}
	if rt.Size() != 0 {
		t.Fatalf("size: got %d, want 0", rt.Size())
	}
	if rt.Remove(id) {
		t.Fatal("second Remove should return false")
	}
}
