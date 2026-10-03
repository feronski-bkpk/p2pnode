package tunnel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMessageStore_AddAndDedup(t *testing.T) {
	s := NewMessageStore(10)
	id, _ := NewMessageID()

	m := Message{
		MessageID: id,
		FromID:    [32]byte{1},
		ToID:      [32]byte{2},
		Text:      "hello",
		Timestamp: time.Now(),
		Direction: Outgoing,
	}

	if !s.Add(m) {
		t.Fatal("first add should succeed")
	}
	if s.Add(m) {
		t.Fatal("second add should be duplicate")
	}
	if !s.Seen(id) {
		t.Fatal("should be seen")
	}
	if s.Count() != 1 {
		t.Fatalf("count: %d", s.Count())
	}
}

func TestMessageStore_Eviction(t *testing.T) {
	s := NewMessageStore(3)
	var ids []MessageID
	for i := 0; i < 5; i++ {
		id, _ := NewMessageID()
		ids = append(ids, id)
		s.Add(Message{MessageID: id, Text: "x"})
	}
	if s.Count() != 3 {
		t.Fatalf("count: %d, want 3", s.Count())
	}
	if s.Seen(ids[0]) {
		t.Fatal("id[0] should be evicted")
	}
	if !s.Seen(ids[4]) {
		t.Fatal("id[4] should be present")
	}
}

func TestMessageStore_ExportJSON(t *testing.T) {
	s := NewMessageStore(10)
	for i := 0; i < 3; i++ {
		id, _ := NewMessageID()
		s.Add(Message{MessageID: id, Text: "hi"})
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")
	if err := s.ExportJSON(path); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file: %v", err)
	}
}
