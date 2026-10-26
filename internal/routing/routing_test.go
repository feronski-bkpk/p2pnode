package routing

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"testing"
)

type checker struct {
	mu           sync.Mutex
	alive        map[ID]bool
	defaultAlive bool
}

func newChecker(def bool) *checker {
	return &checker{alive: make(map[ID]bool), defaultAlive: def}
}

func (c *checker) Set(id ID, alive bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.alive[id] = alive
}

func (c *checker) IsAlive(ct Contact) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.alive[ct.NodeID]; ok {
		return v
	}
	return c.defaultAlive
}

func makeContactAt(t *testing.T, host string, port uint16) Contact {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	c := Contact{
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pub,
		Host:              host,
		Port:              port,
	}
	c.NodeID = IDFromBytes(pub)
	return c
}

func makeContactInBucket(t *testing.T, host string, port uint16) Contact {
	t.Helper()
	for {
		c := makeContactAt(t, host, port)
		if c.NodeID[0] >= 0x80 {
			return c
		}
	}
}

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
	self := randomID(t)
	rt := NewRoutingTable(self, 4)

	var added []Contact
	for i := 0; i < 5; i++ {
		c := makeContactAt(t, "127.0.0.1", uint16(9100+i))
		if !rt.Add(c, nil) {
			t.Fatalf("Add %d failed", i)
		}
		added = append(added, c)
	}
	if rt.Size() != 5 {
		t.Fatalf("size: got %d, want 5", rt.Size())
	}

	target := added[2]
	got, ok := rt.Get(target.NodeID)
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.Port != target.Port {
		t.Fatalf("port: got %d, want %d", got.Port, target.Port)
	}
	if got.Host != target.Host {
		t.Fatalf("host: got %s, want %s", got.Host, target.Host)
	}
}

func TestAddDuplicateMovesToTail(t *testing.T) {
	self := randomID(t)
	rt := NewRoutingTable(self, 4)

	c1 := makeContactAt(t, "127.0.0.1", 9101)
	c2 := makeContactAt(t, "127.0.0.1", 9102)

	rt.Add(c1, nil)
	rt.Add(c2, nil)

	c1b := c1
	c1b.Port = 9999
	rt.Add(c1b, nil)

	if rt.Size() != 2 {
		t.Fatalf("size: %d", rt.Size())
	}
	got, _ := rt.Get(c1.NodeID)
	if got.Port != 9999 {
		t.Fatalf("port not updated: %d", got.Port)
	}
}

func TestAddRejectsSameIDDifferentKey(t *testing.T) {
	self := randomID(t)
	rt := NewRoutingTable(self, 4)

	c1 := makeContactAt(t, "127.0.0.1", 9101)
	rt.Add(c1, nil)

	c2 := c1
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	c2.IdentityPublicKey = otherPub

	if rt.Add(c2, nil) {
		t.Fatal("Add must reject contact with mismatched pubkey")
	}
}

func TestBucketFull_LiveHeadNotEvicted(t *testing.T) {
	k := 3
	var zero ID
	rt := NewRoutingTable(zero, k)
	chk := newChecker(true)

	var sameBucket []Contact
	for i := 0; i < k+1; i++ {
		c := makeContactInBucket(t, "127.0.0.1", uint16(9200+i))
		sameBucket = append(sameBucket, c)
	}

	for i := 0; i < k; i++ {
		if !rt.Add(sameBucket[i], chk) {
			t.Fatalf("Add %d failed", i)
		}
	}
	if rt.Size() != k {
		t.Fatalf("size: %d, want %d", rt.Size(), k)
	}

	added := rt.Add(sameBucket[k], chk)
	if added {
		t.Fatal("Add should return false: live head not evicted")
	}
	if rt.Size() != k {
		t.Fatalf("size after full-add: %d, want %d", rt.Size(), k)
	}
	if _, ok := rt.Get(sameBucket[0].NodeID); !ok {
		t.Fatal("head should still be present")
	}
	if _, ok := rt.Get(sameBucket[k].NodeID); ok {
		t.Fatal("new contact should not be present")
	}
}

func TestBucketFull_DeadHeadEvicted(t *testing.T) {
	k := 3
	var zero ID
	rt := NewRoutingTable(zero, k)
	chk := newChecker(true)

	var sameBucket []Contact
	for i := 0; i < k+1; i++ {
		c := makeContactInBucket(t, "127.0.0.1", uint16(9300+i))
		sameBucket = append(sameBucket, c)
	}

	for i := 0; i < k; i++ {
		rt.Add(sameBucket[i], chk)
	}

	chk.Set(sameBucket[0].NodeID, false)

	added := rt.Add(sameBucket[k], chk)
	if !added {
		t.Fatal("Add should return true: dead head evicted")
	}
	if rt.Size() != k {
		t.Fatalf("size: %d, want %d", rt.Size(), k)
	}
	if _, ok := rt.Get(sameBucket[0].NodeID); ok {
		t.Fatal("dead head should be gone")
	}
	if _, ok := rt.Get(sameBucket[k].NodeID); !ok {
		t.Fatal("new contact should be present")
	}
}

func TestRemove(t *testing.T) {
	self := randomID(t)
	rt := NewRoutingTable(self, 4)
	c := makeContactAt(t, "127.0.0.1", 9101)
	rt.Add(c, nil)
	if !rt.Remove(c.NodeID) {
		t.Fatal("Remove returned false")
	}
	if rt.Size() != 0 {
		t.Fatalf("size: %d", rt.Size())
	}
}

func TestClosest(t *testing.T) {
	self := randomID(t)
	rt := NewRoutingTable(self, 8)

	for i := 0; i < 10; i++ {
		c := makeContactAt(t, "127.0.0.1", uint16(9100+i))
		rt.Add(c, nil)
	}

	target := randomID(t)
	closest := rt.Closest(target, 3)
	if len(closest) != 3 {
		t.Fatalf("got %d, want 3", len(closest))
	}
	all := rt.Closest(target, 100)
	for i := 1; i < len(closest); i++ {
		if CloserTo(target, closest[i].NodeID, closest[i-1].NodeID) {
			t.Fatal("Closest not sorted")
		}
	}
	_ = fmt.Sprintf
	_ = all
}

func TestLocalIDNotAdded(t *testing.T) {
	self := randomID(t)
	rt := NewRoutingTable(self, 4)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	c := Contact{
		NodeID:            self,
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pub,
		Host:              "127.0.0.1",
		Port:              9101,
	}
	if rt.Add(c, nil) {
		t.Fatal("local contact must not be added")
	}
}

func TestInvalidContactRejected(t *testing.T) {
	self := randomID(t)
	rt := NewRoutingTable(self, 4)

	c := Contact{
		NodeID:            randomID(t),
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: []byte("short"),
		Host:              "127.0.0.1",
		Port:              9101,
	}
	if rt.Add(c, nil) {
		t.Fatal("invalid contact must be rejected")
	}
}

func TestSnapshotBuckets(t *testing.T) {
	self := randomID(t)
	rt := NewRoutingTable(self, 4)
	for i := 0; i < 5; i++ {
		c := makeContactAt(t, "127.0.0.1", uint16(9100+i))
		rt.Add(c, nil)
	}
	snaps := rt.SnapshotBuckets()
	total := 0
	for _, s := range snaps {
		total += len(s.Contacts)
	}
	if total != rt.Size() {
		t.Fatalf("snapshot total %d != size %d", total, rt.Size())
	}
}

func TestRemoveIfDead(t *testing.T) {
	self := randomID(t)
	rt := NewRoutingTable(self, 4)
	chk := newChecker(true)
	rt.SetLivenessChecker(chk)

	c := makeContactAt(t, "127.0.0.1", 9101)
	rt.Add(c, nil)

	ctx := context.Background()

	if rt.RemoveIfDead(ctx, c.NodeID) {
		t.Fatal("live contact must not be removed")
	}
	if _, ok := rt.Get(c.NodeID); !ok {
		t.Fatal("live contact should still be present")
	}

	chk.Set(c.NodeID, false)
	if !rt.RemoveIfDead(ctx, c.NodeID) {
		t.Fatal("dead contact must be removed")
	}
	if _, ok := rt.Get(c.NodeID); ok {
		t.Fatal("dead contact should be gone")
	}
}

func TestAddUsesTableLivenessChecker(t *testing.T) {
	k := 3
	var zero ID
	rt := NewRoutingTable(zero, k)
	chk := newChecker(true)
	rt.SetLivenessChecker(chk)

	var sameBucket []Contact
	for i := 0; i < k+1; i++ {
		c := makeContactInBucket(t, "127.0.0.1", uint16(9400+i))
		sameBucket = append(sameBucket, c)
	}

	for i := 0; i < k; i++ {
		if !rt.Add(sameBucket[i], nil) {
			t.Fatalf("Add %d failed", i)
		}
	}

	if rt.Add(sameBucket[k], nil) {
		t.Fatal("Add should return false: live head not evicted (via rt.liveness)")
	}

	b := rt.buckets[bucketIndex(rt.self, sameBucket[0].NodeID)]
	head, ok := b.head()
	if !ok {
		t.Fatal("bucket empty")
	}
	chk.Set(head.NodeID, false)

	if !rt.Add(sameBucket[k], nil) {
		t.Fatalf("Add should return true: dead head %s evicted (via rt.liveness)",
			head.NodeID.Short())
	}
	if _, ok := rt.Get(head.NodeID); ok {
		t.Fatal("dead head should be gone")
	}
	if _, ok := rt.Get(sameBucket[k].NodeID); !ok {
		t.Fatal("new contact should be present")
	}
}

func TestBucketFillsToK_SubnetDiversityDisabled(t *testing.T) {
	k := 4
	var zero ID
	rt := NewRoutingTable(zero, k)
	rt.SetSubnetDiversityLimit(0)

	var sameBucket []Contact
	for i := 0; i < k; i++ {
		host := fmt.Sprintf("node-%02d", i+1)
		c := makeContactInBucket(t, host, uint16(9500+i))
		sameBucket = append(sameBucket, c)
	}

	for i, c := range sameBucket {
		if !rt.Add(c, nil) {
			t.Fatalf("Add %d failed (subnet diversity should be off)", i)
		}
	}
	if rt.Size() != k {
		t.Fatalf("size: got %d, want %d (bucket should fill to K)", rt.Size(), k)
	}
	t.Logf("OK: bucket filled to %d (subnet diversity off)", rt.Size())
}

func TestBucketFillsToK_SubnetLimitEqualsK(t *testing.T) {
	k := 4
	var zero ID
	rt := NewRoutingTable(zero, k)
	rt.SetSubnetDiversityLimit(k)

	var sameBucket []Contact
	for i := 0; i < k; i++ {
		host := fmt.Sprintf("node-%02d", i+1)
		c := makeContactInBucket(t, host, uint16(9600+i))
		sameBucket = append(sameBucket, c)
	}

	for i, c := range sameBucket {
		if !rt.Add(c, nil) {
			t.Fatalf("Add %d failed (limit=K should allow same subnet)", i)
		}
	}
	if rt.Size() != k {
		t.Fatalf("size: got %d, want %d", rt.Size(), k)
	}
	t.Logf("OK: bucket filled to %d with limit=K", rt.Size())
}

func TestBucketSubnetDiversityRejectsThird(t *testing.T) {
	k := 4
	var zero ID
	rt := NewRoutingTable(zero, k)
	rt.SetSubnetDiversityLimit(2)

	var sameZone []Contact
	for i := 0; i < 3; i++ {
		host := fmt.Sprintf("node-%02d", i+1)
		c := makeContactInBucket(t, host, uint16(9700+i))
		sameZone = append(sameZone, c)
	}

	for i := 0; i < 2; i++ {
		if !rt.Add(sameZone[i], nil) {
			t.Fatalf("Add %d failed", i)
		}
	}
	if rt.Add(sameZone[2], nil) {
		t.Fatal("third contact from same subnet should be rejected (limit=2)")
	}
	if rt.Size() != 2 {
		t.Fatalf("size: got %d, want 2", rt.Size())
	}
}

func TestSubnetDiversityLimitAccessors(t *testing.T) {
	var zero ID
	rt := NewRoutingTable(zero, 4)
	if got := rt.SubnetDiversityLimit(); got != 2 {
		t.Fatalf("default limit: got %d, want 2", got)
	}
	rt.SetSubnetDiversityLimit(0)
	if got := rt.SubnetDiversityLimit(); got != 0 {
		t.Fatalf("after set 0: got %d, want 0", got)
	}
	rt.SetSubnetDiversityLimit(5)
	if got := rt.SubnetDiversityLimit(); got != 5 {
		t.Fatalf("after set 5: got %d, want 5", got)
	}
}
