package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"p2pnode/internal/record"
)

func makeKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return pub, priv
}

func makeRecord(t *testing.T, priv ed25519.PrivateKey, seq uint64, ttl time.Duration) *record.NodeRecord {
	t.Helper()
	rec, err := record.New(priv, []string{"127.0.0.1:9001"}, ttl, seq)
	if err != nil {
		t.Fatalf("record.New: %v", err)
	}
	return rec
}

func makeKeyID(b byte) ID {
	var id ID
	id[0] = b
	return id
}

func TestStore_PutGet(t *testing.T) {
	s := New()
	_, priv := makeKey(t)
	rec := makeRecord(t, priv, 1, 3*time.Minute)

	now := time.Now()
	key := makeKeyID(0xAA)

	inserted, err := s.Put(key, rec, now, 3*time.Minute)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !inserted {
		t.Fatal("Put should insert")
	}

	got, ok := s.Get(key, now)
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.SequenceNumber != rec.SequenceNumber {
		t.Fatalf("sequence_number: got %d, want %d", got.SequenceNumber, rec.SequenceNumber)
	}
}

func TestStore_GetExpired(t *testing.T) {
	s := New()
	_, priv := makeKey(t)
	rec := makeRecord(t, priv, 1, 1*time.Second)

	now := time.Now()
	key := makeKeyID(0xBB)
	_, _ = s.Put(key, rec, now, 1*time.Second)

	later := now.Add(2 * time.Second)
	_, ok := s.Get(key, later)
	if ok {
		t.Fatal("Get should return expired record as not found")
	}
}

func TestStore_AntiRollback(t *testing.T) {
	s := New()
	_, priv := makeKey(t)

	rec10 := makeRecord(t, priv, 10, 3*time.Minute)
	rec5 := makeRecord(t, priv, 5, 3*time.Minute)

	now := time.Now()
	key := makeKeyID(0xCC)

	if _, err := s.Put(key, rec10, now, 3*time.Minute); err != nil {
		t.Fatalf("Put rec10: %v", err)
	}

	_, err := s.Put(key, rec5, now, 3*time.Minute)
	if err == nil {
		t.Fatal("want ErrTooOld")
	}

	got, _ := s.Get(key, now)
	if got.SequenceNumber != 10 {
		t.Fatalf("sequence_number: got %d, want 10", got.SequenceNumber)
	}
}

func TestStore_UpdateNewer(t *testing.T) {
	s := New()
	_, priv := makeKey(t)

	rec5 := makeRecord(t, priv, 5, 3*time.Minute)
	rec10 := makeRecord(t, priv, 10, 3*time.Minute)

	now := time.Now()
	key := makeKeyID(0xDD)

	_, _ = s.Put(key, rec5, now, 3*time.Minute)
	inserted, err := s.Put(key, rec10, now, 3*time.Minute)
	if err != nil {
		t.Fatalf("Put rec10: %v", err)
	}
	if !inserted {
		t.Fatal("Put rec10 should update")
	}

	got, _ := s.Get(key, now)
	if got.SequenceNumber != 10 {
		t.Fatalf("sequence_number: got %d, want 10", got.SequenceNumber)
	}
}

func TestStore_SameSequence_NoOverwrite(t *testing.T) {
	s := New()
	_, priv := makeKey(t)

	rec := makeRecord(t, priv, 5, 3*time.Minute)
	now := time.Now()
	key := makeKeyID(0xEE)

	_, _ = s.Put(key, rec, now, 3*time.Minute)

	inserted, err := s.Put(key, rec, now, 3*time.Minute)
	if err != nil {
		t.Fatalf("Put same seq: %v", err)
	}
	if inserted {
		t.Fatal("Put with same sequence_number should return false")
	}
}

func TestStore_Delete(t *testing.T) {
	s := New()
	_, priv := makeKey(t)
	rec := makeRecord(t, priv, 1, 3*time.Minute)
	now := time.Now()
	key := makeKeyID(0xFF)

	_, _ = s.Put(key, rec, now, 3*time.Minute)
	if !s.Delete(key) {
		t.Fatal("Delete should return true")
	}
	if s.Delete(key) {
		t.Fatal("second Delete should return false")
	}
	if s.Size() != 0 {
		t.Fatalf("size: %d", s.Size())
	}
}

func TestStore_Expire(t *testing.T) {
	s := New()
	_, priv := makeKey(t)

	now := time.Now()
	key1 := makeKeyID(0x01)
	key2 := makeKeyID(0x02)

	rec1 := makeRecord(t, priv, 1, 1*time.Second)
	rec2 := makeRecord(t, priv, 2, 10*time.Minute)

	_, _ = s.Put(key1, rec1, now, 1*time.Second)
	_, _ = s.Put(key2, rec2, now, 10*time.Minute)

	later := now.Add(2 * time.Second)
	removed := s.Expire(later)
	if removed != 1 {
		t.Fatalf("Expire: removed %d, want 1", removed)
	}
	if s.Size() != 1 {
		t.Fatalf("size: %d, want 1", s.Size())
	}
}

func TestStore_Keys(t *testing.T) {
	s := New()
	_, priv := makeKey(t)
	now := time.Now()

	for i := byte(1); i <= 3; i++ {
		rec := makeRecord(t, priv, uint64(i), 3*time.Minute)
		_, _ = s.Put(makeKeyID(i), rec, now, 3*time.Minute)
	}

	keys := s.Keys()
	if len(keys) != 3 {
		t.Fatalf("keys: %d, want 3", len(keys))
	}
}

func TestStore_PutInvalidRecord(t *testing.T) {
	s := New()
	_, priv := makeKey(t)
	rec := makeRecord(t, priv, 1, 3*time.Minute)
	rec.NodeID[0] ^= 0xFF

	_, err := s.Put(makeKeyID(0xAA), rec, time.Now(), 3*time.Minute)
	if err == nil {
		t.Fatal("Put with invalid record should fail")
	}
}

func TestStore_Snapshot(t *testing.T) {
	s := New()
	_, priv := makeKey(t)
	now := time.Now()

	rec := makeRecord(t, priv, 1, 3*time.Minute)
	_, _ = s.Put(makeKeyID(0x01), rec, now, 3*time.Minute)

	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot: %d, want 1", len(snap))
	}

	for _, v := range snap {
		v.Record.SequenceNumber = 999
	}
	got, _ := s.Get(makeKeyID(0x01), now)
	if got.SequenceNumber == 999 {
		t.Fatal("snapshot mutation leaked to store")
	}
}
