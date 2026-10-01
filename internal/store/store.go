package store

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"p2pnode/internal/record"
)

type ID [32]byte

func (id ID) String() string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(id)*2)
	for i, b := range id {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0F]
	}
	return string(out)
}

func (id ID) Short() string {
	s := id.String()
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func (id ID) IsZero() bool {
	for _, b := range id {
		if b != 0 {
			return false
		}
	}
	return true
}

var (
	ErrTooOld   = errors.New("store: sequence_number too old")
	ErrNotFound = errors.New("store: not found")
	ErrExpired  = errors.New("store: record expired")
)

type StoredRecord struct {
	Record    *record.NodeRecord
	StoredAt  time.Time
	ExpiresAt time.Time
}

type Store struct {
	mu      sync.RWMutex
	records map[ID]*StoredRecord
}

func New() *Store {
	return &Store{
		records: make(map[ID]*StoredRecord),
	}
}

func (s *Store) Put(key ID, rec *record.NodeRecord, now time.Time, ttl time.Duration) (bool, error) {
	if rec == nil {
		return false, errors.New("store: nil record")
	}
	if err := rec.Validate(now); err != nil {
		return false, fmt.Errorf("store: validate: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[key]
	if ok {
		if rec.SequenceNumber < existing.Record.SequenceNumber {
			return false, fmt.Errorf("%w: got %d, have %d",
				ErrTooOld, rec.SequenceNumber, existing.Record.SequenceNumber)
		}
		if rec.SequenceNumber == existing.Record.SequenceNumber {
			existing.ExpiresAt = now.Add(ttl)
			return false, nil
		}
		existing.Record = rec
		existing.StoredAt = now
		existing.ExpiresAt = now.Add(ttl)
		return true, nil
	}

	s.records[key] = &StoredRecord{
		Record:    rec,
		StoredAt:  now,
		ExpiresAt: now.Add(ttl),
	}
	return true, nil
}

func (s *Store) Get(key ID, now time.Time) (*record.NodeRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sr, ok := s.records[key]
	if !ok {
		return nil, false
	}
	if sr.ExpiresAt.Before(now) {
		return nil, false
	}
	return sr.Record, true
}

func (s *Store) GetStored(key ID, now time.Time) (*StoredRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sr, ok := s.records[key]
	if !ok {
		return nil, false
	}
	if sr.ExpiresAt.Before(now) {
		return nil, false
	}
	cp := *sr
	return &cp, true
}

func (s *Store) Delete(key ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.records[key]
	if ok {
		delete(s.records, key)
	}
	return ok
}

func (s *Store) Expire(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for k, sr := range s.records {
		if sr.ExpiresAt.Before(now) {
			delete(s.records, k)
			removed++
		}
	}
	return removed
}

func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

func (s *Store) Keys() []ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ID, 0, len(s.records))
	for k := range s.records {
		out = append(out, k)
	}
	return out
}

func (s *Store) Snapshot() map[ID]*StoredRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[ID]*StoredRecord, len(s.records))
	for k, v := range s.records {
		out[k] = &StoredRecord{
			Record:    cloneRecord(v.Record),
			StoredAt:  v.StoredAt,
			ExpiresAt: v.ExpiresAt,
		}
	}
	return out
}

func cloneRecord(r *record.NodeRecord) *record.NodeRecord {
	if r == nil {
		return nil
	}
	cp := *r
	if r.IdentityPubKey != nil {
		cp.IdentityPubKey = append([]byte(nil), r.IdentityPubKey...)
	}
	if r.Addresses != nil {
		cp.Addresses = append([]string(nil), r.Addresses...)
	}
	if r.Signature != nil {
		cp.Signature = append([]byte(nil), r.Signature...)
	}
	return &cp
}

func (s *Store) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("Store{size=%d}", len(s.records))
}
