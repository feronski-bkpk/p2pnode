package tunnel

import (
	"sync"
	"time"
)

type ProfileStore struct {
	mu       sync.RWMutex
	profiles map[[32]byte]*RelayProfile
}

type RelayProfile struct {
	NodeID              [32]byte      `json:"node_id"`
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
