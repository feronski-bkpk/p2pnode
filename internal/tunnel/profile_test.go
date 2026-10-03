package tunnel

import (
	"testing"
	"time"
)

func TestProfileStore_RecordSuccess(t *testing.T) {
	s := NewProfileStore()
	id := [32]byte{0xAA}

	s.RecordSuccess(id, 10*time.Millisecond)
	p, ok := s.Get(id)
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Successes != 1 {
		t.Fatalf("successes: %d", p.Successes)
	}
	if p.Failures != 0 {
		t.Fatalf("failures: %d", p.Failures)
	}
	if p.SuccessEMA <= 0.5 {
		t.Fatalf("success ema should grow: %f", p.SuccessEMA)
	}
	if p.LatencyEMA != 10*time.Millisecond {
		t.Fatalf("latency ema: %v", p.LatencyEMA)
	}
}

func TestProfileStore_RecordFailure(t *testing.T) {
	s := NewProfileStore()
	id := [32]byte{0xBB}

	s.RecordFailure(id)
	p, _ := s.Get(id)
	if p.Failures != 1 {
		t.Fatalf("failures: %d", p.Failures)
	}
	if p.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive: %d", p.ConsecutiveFailures)
	}
	if p.SuccessEMA >= 0.5 {
		t.Fatalf("success ema should decrease: %f", p.SuccessEMA)
	}
}

func TestProfileStore_ConsecutiveReset(t *testing.T) {
	s := NewProfileStore()
	id := [32]byte{0xCC}

	s.RecordFailure(id)
	s.RecordFailure(id)
	p, _ := s.Get(id)
	if p.ConsecutiveFailures != 2 {
		t.Fatalf("consecutive: %d", p.ConsecutiveFailures)
	}

	s.RecordSuccess(id, time.Millisecond)
	p, _ = s.Get(id)
	if p.ConsecutiveFailures != 0 {
		t.Fatalf("consecutive should reset: %d", p.ConsecutiveFailures)
	}
}

func TestProfileStore_Rank(t *testing.T) {
	s := NewProfileStore()
	good := [32]byte{0x01}
	bad := [32]byte{0x02}
	unknown := [32]byte{0x03}

	for i := 0; i < 5; i++ {
		s.RecordSuccess(good, 10*time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		s.RecordFailure(bad)
	}

	ranked := s.Rank([][32]byte{bad, unknown, good})
	if ranked[0] != good {
		t.Fatalf("first should be good, got %x", ranked[0][:2])
	}
	if ranked[2] != bad {
		t.Fatalf("last should be bad, got %x", ranked[2][:2])
	}
}

func TestProfileStore_Empty(t *testing.T) {
	s := NewProfileStore()
	_, ok := s.Get([32]byte{0xFF})
	if ok {
		t.Fatal("should not find")
	}
	in := [][32]byte{{1}, {2}}
	out := s.Rank(in)
	if len(out) != 2 {
		t.Fatalf("len: %d", len(out))
	}
}
