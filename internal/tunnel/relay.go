package tunnel

import (
	"sync"
	"time"

	"p2pnode/internal/protocol"
)

type RawConn interface {
	ReadFrame() (protocol.Frame, error)
	WriteFrame(protocol.Frame) error
	RemoteAddr() string
	Close() error
}

type RelayState struct {
	TunnelID ID

	PrevConn RawConn
	NextAddr string
	NextConn RawConn

	HopIndex  uint8
	ExpiresAt time.Time

	mu sync.Mutex
}

func (rs *RelayState) SetNextConn(c RawConn) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.NextConn = c
}

func (rs *RelayState) GetNextConn() RawConn {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.NextConn
}

func (rs *RelayState) Expired() bool {
	if rs.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(rs.ExpiresAt)
}

func (rs *RelayState) Close() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.NextConn != nil {
		_ = rs.NextConn.Close()
		rs.NextConn = nil
	}
	if rs.PrevConn != nil {
		_ = rs.PrevConn.Close()
		rs.PrevConn = nil
	}
}

type RelayStore struct {
	mu    sync.RWMutex
	state map[ID]*RelayState
}

func NewRelayStore() *RelayStore {
	return &RelayStore{state: make(map[ID]*RelayState)}
}

func (s *RelayStore) Put(rs *RelayState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.state[rs.TunnelID]; ok {
		old.Close()
	}
	s.state[rs.TunnelID] = rs
}

func (s *RelayStore) Get(id ID) (*RelayState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rs, ok := s.state[id]
	return rs, ok
}

func (s *RelayStore) Remove(id ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rs, ok := s.state[id]; ok {
		rs.Close()
		delete(s.state, id)
	}
}

func (s *RelayStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.state)
}

func (s *RelayStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rs := range s.state {
		rs.Close()
	}
	s.state = make(map[ID]*RelayState)
}

func (s *RelayStore) ExpireAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, rs := range s.state {
		if rs.Expired() {
			rs.Close()
			delete(s.state, id)
			removed++
		}
	}
	return removed
}

type DestSession struct {
	E2E       *E2ESession
	ExpiresAt time.Time
}

type DestSessionStore struct {
	mu       sync.RWMutex
	sessions map[ID]*DestSession
}

func NewDestSessionStore() *DestSessionStore {
	return &DestSessionStore{sessions: make(map[ID]*DestSession)}
}

func (s *DestSessionStore) Put(id ID, ds *DestSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = ds
}

func (s *DestSessionStore) Get(id ID) (*DestSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ds, ok := s.sessions[id]
	return ds, ok
}

func (s *DestSessionStore) Remove(id ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *DestSessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

func (s *DestSessionStore) ExpireAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, ds := range s.sessions {
		if !ds.ExpiresAt.IsZero() && time.Now().After(ds.ExpiresAt) {
			delete(s.sessions, id)
			removed++
		}
	}
	return removed
}
