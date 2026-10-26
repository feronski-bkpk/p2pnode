package rpc

import (
	"context"
	"sync"
	"time"

	"p2pnode/internal/routing"
)

const (
	defaultLivenessTTL      = 15 * time.Second
	defaultProbeConcurrency = 8
)

type livenessEntry struct {
	alive     bool
	checkedAt time.Time
	inFlight  bool
}

type PingChecker struct {
	Client  *Client
	Local   routing.Contact
	Timeout time.Duration
	TTL     time.Duration

	mu       sync.Mutex
	cache    map[routing.ID]*livenessEntry
	sem      chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
}

func NewPingChecker(client *Client, local routing.Contact, timeout time.Duration) *PingChecker {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	c := &PingChecker{
		Client:  client,
		Local:   local,
		Timeout: timeout,
		TTL:     defaultLivenessTTL,
		cache:   make(map[routing.ID]*livenessEntry),
		sem:     make(chan struct{}, defaultProbeConcurrency),
		stop:    make(chan struct{}),
	}
	return c
}

func (c *PingChecker) Close() {
	c.stopOnce.Do(func() { close(c.stop) })
}

func (c *PingChecker) IsAlive(ct routing.Contact) bool {
	c.mu.Lock()
	e := c.cache[ct.NodeID]
	if e != nil {
		fresh := time.Since(e.checkedAt) < c.TTL
		alive := e.alive
		if fresh || e.inFlight {
			c.mu.Unlock()
			return alive
		}
		e.inFlight = true
		c.mu.Unlock()
		go c.probe(ct)
		return alive
	}
	c.cache[ct.NodeID] = &livenessEntry{alive: true, checkedAt: time.Time{}, inFlight: true}
	c.mu.Unlock()
	go c.probe(ct)
	return true
}

func (c *PingChecker) IsAliveSync(ctx context.Context, ct routing.Contact) bool {
	c.mu.Lock()
	e := c.cache[ct.NodeID]
	if e != nil && time.Since(e.checkedAt) < c.TTL {
		alive := e.alive
		c.mu.Unlock()
		return alive
	}
	if e != nil && e.inFlight {
		c.mu.Unlock()
		return c.waitForResult(ctx, ct.NodeID)
	}
	if e == nil {
		e = &livenessEntry{}
		c.cache[ct.NodeID] = e
	}
	e.inFlight = true
	c.mu.Unlock()

	c.probeWithCtx(ctx, ct)
	return c.lastAlive(ct.NodeID)
}

func (c *PingChecker) probe(ct routing.Contact) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	c.probeWithCtx(ctx, ct)
}

func (c *PingChecker) probeWithCtx(ctx context.Context, ct routing.Contact) {
	select {
	case c.sem <- struct{}{}:
	case <-c.stop:
		c.setResult(ct.NodeID, false, false)
		return
	case <-ctx.Done():
		c.setResult(ct.NodeID, false, false)
		return
	}
	defer func() { <-c.sem }()

	expected := ct.NodeID
	_, err := c.Client.Ping(ctx, c.Local, ct.Addr(), &expected, c.Timeout)
	c.setResult(ct.NodeID, err == nil, true)
}

func (c *PingChecker) setResult(id routing.ID, alive, checked bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[id]
	if !ok {
		e = &livenessEntry{}
		c.cache[id] = e
	}
	e.alive = alive
	e.inFlight = false
	if checked {
		e.checkedAt = time.Now()
	}
}

func (c *PingChecker) lastAlive(id routing.ID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.cache[id]; ok {
		return e.alive
	}
	return false
}

func (c *PingChecker) waitForResult(ctx context.Context, id routing.ID) bool {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return c.lastAlive(id)
		case <-c.stop:
			return c.lastAlive(id)
		case <-tick.C:
			c.mu.Lock()
			e, ok := c.cache[id]
			done := ok && !e.inFlight
			alive := false
			if ok {
				alive = e.alive
			}
			c.mu.Unlock()
			if done {
				return alive
			}
		}
	}
}

func (c *PingChecker) Invalidate(id routing.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, id)
}
