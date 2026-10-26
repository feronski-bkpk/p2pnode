package routing

import (
	"context"
	"fmt"
	"net"
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

	subnetLimit int

	mu       sync.RWMutex
	buckets  [IDBits]*bucket
	observer Observer
	liveness LivenessChecker
}

func NewRoutingTable(self ID, k int) *RoutingTable {
	if k <= 0 {
		k = 4
	}
	rt := &RoutingTable{self: self, k: k, subnetLimit: 2}
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

func (rt *RoutingTable) SetLivenessChecker(c LivenessChecker) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.liveness = c
}

func (rt *RoutingTable) livenessChecker(explicit LivenessChecker) LivenessChecker {
	if explicit != nil {
		return explicit
	}
	return rt.liveness
}

func (rt *RoutingTable) SetSubnetDiversityLimit(n int) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.subnetLimit = n
}

func (rt *RoutingTable) SubnetDiversityLimit() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.subnetLimit
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

	c.Touch()

	rt.mu.Lock()

	b := rt.buckets[idx]

	if rt.subnetLimit > 0 {
		if newSubnet := subnetOfContact(c); newSubnet != "" {
			sameSubnet := 0
			for _, existing := range b.contacts {
				if subnetOfContact(existing) == newSubnet {
					sameSubnet++
				}
			}
			if sameSubnet >= rt.subnetLimit {
				if b.find(c.NodeID) < 0 {
					rt.mu.Unlock()
					if rt.observer != nil {
						rt.observer.ContactRemoved(c.NodeID, c.Addr(), "subnet_diversity")
					}
					return false
				}
			}
		}
	}

	if existing := b.find(c.NodeID); existing >= 0 {
		old := b.contacts[existing]
		if !old.SameIdentity(c) {
			rt.mu.Unlock()
			return false
		}
		b.update(c)
		rt.mu.Unlock()
		if rt.observer != nil {
			rt.observer.ContactUpdated(c.NodeID, c.Addr(), idx)
		}
		return true
	}

	if b.len() < rt.k {
		b.appendTail(c)
		rt.mu.Unlock()
		if rt.observer != nil {
			rt.observer.ContactAdded(c.NodeID, c.Addr(), idx)
		}
		return true
	}

	head, ok := b.head()
	if !ok {
		b.appendTail(c)
		rt.mu.Unlock()
		if rt.observer != nil {
			rt.observer.ContactAdded(c.NodeID, c.Addr(), idx)
		}
		return true
	}

	chk := rt.livenessChecker(checker)
	rt.mu.Unlock()

	alive := false
	if chk != nil {
		alive = chk.IsAlive(head)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	b = rt.buckets[idx]

	if b.find(head.NodeID) < 0 {
		if b.find(c.NodeID) >= 0 {
			return false
		}
		if b.len() < rt.k {
			b.appendTail(c)
			if rt.observer != nil {
				rt.observer.ContactAdded(c.NodeID, c.Addr(), idx)
			}
			return true
		}
		newHead, _ := b.head()
		b.removeHead()
		b.appendTail(c)
		if rt.observer != nil {
			rt.observer.ContactRemoved(newHead.NodeID, newHead.Addr(), "evicted")
			rt.observer.ContactAdded(c.NodeID, c.Addr(), idx)
		}
		return true
	}

	if alive {
		b.moveToTail(head.NodeID)
		if i := b.find(head.NodeID); i >= 0 {
			b.contacts[i].Touch()
		}
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

func subnetOfContact(c Contact) string {
	host := c.Host
	if host == "" {
		return ""
	}

	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return ""
	}

	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() {
			return ""
		}
		ip4 := ip.To4()
		if ip4 == nil {
			return ip.String() + "/64"
		}
		return net.IPv4(ip4[0], ip4[1], ip4[2], 0).String() + "/24"
	}

	var n int
	if _, err := fmt.Sscanf(host, "node-%02d", &n); err == nil && n > 0 {
		zone := (n-1)/3 + 1
		return fmt.Sprintf("zone-%d/24", zone)
	}

	if len(host) >= 6 && host[:6] == "sybil-" {
		return "zone-sybil/24"
	}

	return ""
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

func (rt *RoutingTable) RemoveIfDead(ctx context.Context, id ID) bool {
	idx := bucketIndex(rt.self, id)
	if idx < 0 {
		return false
	}

	rt.mu.RLock()
	b := rt.buckets[idx]
	i := b.find(id)
	if i < 0 {
		rt.mu.RUnlock()
		return false
	}
	c := b.contacts[i]
	chk := rt.liveness
	rt.mu.RUnlock()

	alive := false
	if chk != nil {
		if syncChecker, ok := chk.(interface {
			IsAliveSync(ctx context.Context, ct Contact) bool
		}); ok {
			alive = syncChecker.IsAliveSync(ctx, c)
		} else {
			alive = chk.IsAlive(c)
		}
	}

	if alive {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		b = rt.buckets[idx]
		if j := b.find(id); j >= 0 {
			b.moveToTail(id)
			b.contacts[len(b.contacts)-1].Touch()
			if rt.observer != nil {
				rt.observer.ContactUpdated(c.NodeID, c.Addr(), idx)
			}
		}
		return false
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	b = rt.buckets[idx]
	i = b.find(id)
	if i < 0 {
		return false
	}
	c = b.contacts[i]
	copy(b.contacts[i:], b.contacts[i+1:])
	b.contacts = b.contacts[:len(b.contacts)-1]
	if rt.observer != nil {
		rt.observer.ContactRemoved(c.NodeID, c.Addr(), "dead")
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
