package filetransfer

import (
	"sync"
	"time"
)

type InProgressTransfer struct {
	Header    *Header
	Chunks    map[uint32]Chunk
	StartedAt time.Time
	UpdatedAt time.Time
}

type Store struct {
	mu        sync.Mutex
	transfers map[FileID]*InProgressTransfer
	ttl       time.Duration
}

const TransferTTL = 10 * time.Minute

func NewStore() *Store {
	return &Store{
		transfers: make(map[FileID]*InProgressTransfer),
		ttl:       TransferTTL,
	}
}

func (s *Store) Init(h *Header) *InProgressTransfer {
	s.mu.Lock()
	defer s.mu.Unlock()

	if t, ok := s.transfers[h.FileID]; ok {
		t.UpdatedAt = time.Now()
		return t
	}
	now := time.Now()
	t := &InProgressTransfer{
		Header:    h,
		Chunks:    make(map[uint32]Chunk, h.TotalChunks),
		StartedAt: now,
		UpdatedAt: now,
	}
	s.transfers[h.FileID] = t
	return t
}

func (s *Store) AddChunk(c Chunk) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.transfers[c.FileID]
	if !ok {
		return false
	}
	if _, exists := t.Chunks[c.ChunkIndex]; exists {
		return false
	}
	t.Chunks[c.ChunkIndex] = c
	t.UpdatedAt = time.Now()
	return true
}

func (s *Store) Complete(id FileID) *InProgressTransfer {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.transfers[id]
	if !ok {
		return nil
	}
	if uint32(len(t.Chunks)) != t.Header.TotalChunks {
		return nil
	}
	delete(s.transfers, id)
	return t
}

func (s *Store) Get(id FileID) *InProgressTransfer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transfers[id]
}

func (s *Store) Expire() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	n := 0
	for id, t := range s.transfers {
		if now.Sub(t.UpdatedAt) > s.ttl {
			delete(s.transfers, id)
			n++
		}
	}
	return n
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.transfers)
}
