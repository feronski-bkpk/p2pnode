package events

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	l, err := NewLogger(path, "abc12345")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}

	l.Log("server_started", map[string]any{"addr": "127.0.0.1:9001"})
	l.LogPeer("frame_sent", "def67890", "127.0.0.1:9002", map[string]any{
		"type": "PING",
		"size": 42,
	})
	l.Log("contact_added", map[string]any{"bucket": 3})

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var events []Event
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		events = append(events, ev)
	}

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	if events[0].Kind != "server_started" {
		t.Fatalf("event[0].Kind = %s", events[0].Kind)
	}
	if events[0].NodeID != "abc12345" {
		t.Fatalf("event[0].NodeID = %s", events[0].NodeID)
	}
	if events[0].Ts <= 0 {
		t.Fatalf("event[0].Ts = %f", events[0].Ts)
	}

	if events[1].Kind != "frame_sent" {
		t.Fatalf("event[1].Kind = %s", events[1].Kind)
	}
	if events[1].Peer != "def67890" {
		t.Fatalf("event[1].Peer = %s", events[1].Peer)
	}
	if events[1].Addr != "127.0.0.1:9002" {
		t.Fatalf("event[1].Addr = %s", events[1].Addr)
	}
}

func TestLogger_NoOpMode(t *testing.T) {
	l, err := NewLogger("", "abc12345")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	l.Log("test", nil)
	l.LogPeer("test", "x", "y", nil)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestLogger_NilSafe(t *testing.T) {
	var l *Logger
	l.Log("test", nil)
	l.LogPeer("test", "x", "y", nil)
	if err := l.Close(); err != nil {
		t.Fatalf("Close on nil: %v", err)
	}
}

func TestLogger_AppendMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	l1, _ := NewLogger(path, "abc12345")
	l1.Log("first", nil)
	l1.Close()

	l2, _ := NewLogger(path, "abc12345")
	l2.Log("second", nil)
	l2.Close()

	buf, _ := os.ReadFile(path)
	content := string(buf)
	if !strings.Contains(content, `"event":"first"`) {
		t.Fatal("first event lost")
	}
	if !strings.Contains(content, `"event":"second"`) {
		t.Fatal("second event missing")
	}
}
