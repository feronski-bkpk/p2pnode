package dht

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	// размер k-bucket.
	K = 8
	// параллелизм итеративного поиска.
	Alpha = 3
	// через сколько без PONG узел считается мёртвым.
	NodeTimeout = 30 * time.Second
)

type bucket struct {
	nodes []Node
}

type RoutingTable struct {
	self    ID
	mu      sync.RWMutex
	buckets [IDBits]*bucket
}

func NewRoutingTable(self ID) *RoutingTable {
	rt := &RoutingTable{self: self}
	for i := range rt.buckets {
		rt.buckets[i] = &bucket{}
	}
	return rt
}

func bucketIndex(self, other ID) int {
	for i := 0; i < IDLen; i++ {
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

func (rt *RoutingTable) Add(n Node) bool {
	if n.ID == rt.self {
		return false
	}
	if n.ID.IsZero() {
		return false
	}
	if n.Addr == "" {
		return false
	}
	idx := bucketIndex(rt.self, n.ID)
	if idx < 0 {
		return false
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	b := rt.buckets[idx]
	n.Touch()

	for i := range b.nodes {
		if b.nodes[i].ID == n.ID {
			b.nodes[i].Addr = n.Addr
			b.nodes[i].Touch()
			moved := b.nodes[i]
			copy(b.nodes[i:], b.nodes[i+1:])
			b.nodes[len(b.nodes)-1] = moved
			return true
		}
	}

	if len(b.nodes) < K {
		b.nodes = append(b.nodes, n)
		return true
	}

	copy(b.nodes, b.nodes[1:])
	b.nodes[len(b.nodes)-1] = n
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
	for i := range b.nodes {
		if b.nodes[i].ID == id {
			b.nodes = append(b.nodes[:i], b.nodes[i+1:]...)
			return true
		}
	}
	return false
}

func (rt *RoutingTable) Get(id ID) (Node, bool) {
	idx := bucketIndex(rt.self, id)
	if idx < 0 {
		return Node{}, false
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	for _, n := range rt.buckets[idx].nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

func (rt *RoutingTable) Closest(target ID, n int) []Node {
	rt.mu.RLock()
	all := make([]Node, 0, K*8)
	for _, b := range rt.buckets {
		all = append(all, b.nodes...)
	}
	rt.mu.RUnlock()

	sort.Slice(all, func(i, j int) bool {
		return CloserTo(target, all[i].ID, all[j].ID)
	})
	if len(all) > n {
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
		total += len(b.nodes)
	}
	return total
}

func (rt *RoutingTable) Snapshot() []Node {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	out := make([]Node, 0, rt.sizeLocked())
	for _, b := range rt.buckets {
		out = append(out, b.nodes...)
	}
	return out
}

func (rt *RoutingTable) String() string {
	return fmt.Sprintf("RoutingTable{self=%s, size=%d}", rt.self, rt.Size())
}
