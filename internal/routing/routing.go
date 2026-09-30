package routing

import (
	"fmt"
	"sort"
	"sync"
)

type Observer interface {
	ContactAdded(peer ID, addr string, bucketIdx int)
	ContactUpdated(peer ID, addr string, bucketIdx int)
	ContactRemoved(peer ID, addr string, reason string)
}

type LivenessChecker interface {
	IsAlive(c Contact) bool
}

const IDBits = 256

type RoutingTable struct {
	self ID
	k    int

	mu       sync.RWMutex
	buckets  [IDBits]*bucket
	observer Observer
}

func NewRoutingTable(self ID, k int) *RoutingTable {
	if k <= 0 {
		k = 4
	}
	rt := &RoutingTable{self: self, k: k}
	for i := range rt.buckets {
		rt.buckets[i] = newBucket()
	}
	return rt
}

func (rt *RoutingTable) SetObserver(o Observer) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.observer = o
}

func (rt *RoutingTable) Self() ID { return rt.self }

func (rt *RoutingTable) K() int { return rt.k }

func bucketIndex(self, other ID) int {
	for i := range self {
		x := self[i] ^ other[i]
		if x == 0 {
			continue
		}
		for bit := 7; bit >= 0; bit-- {
			if x&(1<<bit) != 0 {
				return i*8 + (7 - bit)
			}
		}
	}
	return -1
}

func (rt *RoutingTable) Add(c Contact, checker LivenessChecker) bool {
	if err := c.Validate(); err != nil {
		return false
	}
	if c.NodeID == rt.self {
		return false
	}
	idx := bucketIndex(rt.self, c.NodeID)
	if idx < 0 {
		return false
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	b := rt.buckets[idx]
	c.Touch()

	if existing := b.find(c.NodeID); existing >= 0 {
		old := b.contacts[existing]
		if !old.SameIdentity(c) {
			return false
		}
		b.update(c)
		if rt.observer != nil {
			rt.observer.ContactUpdated(c.NodeID, c.Addr(), idx)
		}
		return true
	}

	if b.len() < rt.k {
		b.appendTail(c)
		if rt.observer != nil {
			rt.observer.ContactAdded(c.NodeID, c.Addr(), idx)
		}
		return true
	}

	head, ok := b.head()
	if !ok {
		b.appendTail(c)
		if rt.observer != nil {
			rt.observer.ContactAdded(c.NodeID, c.Addr(), idx)
		}
		return true
	}

	alive := false
	if checker != nil {
		alive = checker.IsAlive(head)
	}
	if alive {
		b.moveToTail(head.NodeID)
		return false
	}

	b.removeHead()
	b.appendTail(c)
	if rt.observer != nil {
		rt.observer.ContactRemoved(head.NodeID, head.Addr(), "evicted")
		rt.observer.ContactAdded(c.NodeID, c.Addr(), idx)
	}
	return true
}

func (rt *RoutingTable) Remove(id ID) bool {
	idx := bucketIndex(rt.self, id)
	if idx < 0 {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	b := rt.buckets[idx]
	i := b.find(id)
	if i < 0 {
		return false
	}
	removed := b.contacts[i]
	copy(b.contacts[i:], b.contacts[i+1:])
	b.contacts = b.contacts[:len(b.contacts)-1]
	if rt.observer != nil {
		rt.observer.ContactRemoved(removed.NodeID, removed.Addr(), "removed")
	}
	return true
}

func (rt *RoutingTable) Get(id ID) (Contact, bool) {
	idx := bucketIndex(rt.self, id)
	if idx < 0 {
		return Contact{}, false
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	b := rt.buckets[idx]
	i := b.find(id)
	if i < 0 {
		return Contact{}, false
	}
	return b.contacts[i], true
}

func (rt *RoutingTable) Closest(target ID, n int) []Contact {
	rt.mu.RLock()
	all := make([]Contact, 0, rt.k*8)
	for _, b := range rt.buckets {
		all = append(all, b.contacts...)
	}
	rt.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool {
		return CloserTo(target, all[i].NodeID, all[j].NodeID)
	})
	if n > 0 && len(all) > n {
		all = all[:n]
	}
	return all
}

func (rt *RoutingTable) Size() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.sizeLocked()
}

func (rt *RoutingTable) sizeLocked() int {
	total := 0
	for _, b := range rt.buckets {
		total += b.len()
	}
	return total
}

func (rt *RoutingTable) Snapshot() []Contact {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	out := make([]Contact, 0, rt.sizeLocked())
	for _, b := range rt.buckets {
		out = append(out, b.contacts...)
	}
	return out
}

type BucketSnapshot struct {
	Index    int       `json:"index"`
	Contacts []Contact `json:"contacts"`
}

func (rt *RoutingTable) SnapshotBuckets() []BucketSnapshot {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var out []BucketSnapshot
	for i, b := range rt.buckets {
		if b.len() == 0 {
			continue
		}
		out = append(out, BucketSnapshot{
			Index:    i,
			Contacts: b.snapshot(),
		})
	}
	return out
}

func (rt *RoutingTable) String() string {
	return fmt.Sprintf("RoutingTable{self=%s, k=%d, size=%d}",
		rt.self.Short(), rt.k, rt.Size())
}
