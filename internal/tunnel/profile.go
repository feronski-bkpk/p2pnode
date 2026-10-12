package tunnel

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type ProfileStore struct {
	mu       sync.RWMutex
	profiles map[[32]byte]*RelayProfile
}

type RelayProfile struct {
	NodeID              [32]byte      `json:"node_id"`
	Subnet              string        `json:"subnet"`
	Successes           uint64        `json:"successes"`
	Failures            uint64        `json:"failures"`
	SuccessEMA          float64       `json:"success_ema"`
	LatencyEMA          time.Duration `json:"latency_ema_ns"`
	LastSeen            time.Time     `json:"last_seen"`
	ConsecutiveFailures uint8         `json:"consecutive_failures"`
}

const (
	emaAlpha          = 0.2
	defaultSuccessEMA = 0.5
	defaultLatencyEMA = 100 * time.Millisecond
)

func NewProfileStore() *ProfileStore {
	return &ProfileStore{
		profiles: make(map[[32]byte]*RelayProfile),
	}
}

func (s *ProfileStore) RecordSuccess(id [32]byte, rtt time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.getOrCreate(id)
	p.Successes++
	p.LastSeen = time.Now()
	p.ConsecutiveFailures = 0

	p.SuccessEMA = emaAlpha*1.0 + (1-emaAlpha)*p.SuccessEMA

	if p.LatencyEMA == 0 {
		p.LatencyEMA = rtt
	} else {
		p.LatencyEMA = time.Duration(float64(emaAlpha)*float64(rtt) +
			(1-emaAlpha)*float64(p.LatencyEMA))
	}
}

func (s *ProfileStore) RecordFailure(id [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.getOrCreate(id)
	p.Failures++
	p.LastSeen = time.Now()
	p.ConsecutiveFailures++

	p.SuccessEMA = (1 - emaAlpha) * p.SuccessEMA
}

func (s *ProfileStore) RecordAddr(id [32]byte, addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.getOrCreate(id)
	p.Subnet = subnetOf(addr)
}

func (s *ProfileStore) Get(id [32]byte) (*RelayProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.profiles[id]
	if !ok {
		return nil, false
	}
	cp := *p
	return &cp, true
}

func (s *ProfileStore) Rank(candidates [][32]byte) [][32]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		id    [32]byte
		score float64
	}
	scoredList := make([]scored, 0, len(candidates))
	for _, id := range candidates {
		p, ok := s.profiles[id]
		var score float64
		if !ok {
			score = defaultSuccessEMA
		} else {
			lat := p.LatencyEMA
			if lat == 0 {
				lat = defaultLatencyEMA
			}
			latencyPenalty := float64(lat) / float64(time.Second)
			if latencyPenalty > 1.0 {
				latencyPenalty = 1.0
			}
			score = p.SuccessEMA - latencyPenalty
			score -= float64(p.ConsecutiveFailures) * 0.1
		}
		scoredList = append(scoredList, scored{id, score})
	}

	for i := 1; i < len(scoredList); i++ {
		for j := i; j > 0 && scoredList[j].score > scoredList[j-1].score; j-- {
			scoredList[j], scoredList[j-1] = scoredList[j-1], scoredList[j]
		}
	}

	out := make([][32]byte, len(scoredList))
	for i, s := range scoredList {
		out[i] = s.id
	}
	return out
}

func (s *ProfileStore) SubnetOf(id [32]byte) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.profiles[id]; ok {
		return p.Subnet
	}
	return ""
}

func (s *ProfileStore) Snapshot() []RelayProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RelayProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, *p)
	}
	return out
}

func (s *ProfileStore) getOrCreate(id [32]byte) *RelayProfile {
	p, ok := s.profiles[id]
	if !ok {
		p = &RelayProfile{
			NodeID:     id,
			SuccessEMA: defaultSuccessEMA,
			LatencyEMA: 0,
		}
		s.profiles[id] = p
	}
	return p
}

func subnetOf(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return ""
	}

	if ip := net.ParseIP(host); ip != nil {
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
	return ""
}
