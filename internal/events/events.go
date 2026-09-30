package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	Ts      float64        `json:"ts"`
	NodeID  string         `json:"node_id"`
	Kind    string         `json:"event"`
	Peer    string         `json:"peer,omitempty"`
	Addr    string         `json:"addr,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type Logger struct {
	mu   sync.Mutex
	f    *os.File
	enc  *json.Encoder
	id   string
	path string
}

func NewLogger(path, nodeIDShort string) (*Logger, error) {
	if path == "" {
		return &Logger{id: nodeIDShort}, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("events: mkdir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("events: open %s: %w", path, err)
	}

	return &Logger{
		f:    f,
		enc:  json.NewEncoder(f),
		id:   nodeIDShort,
		path: path,
	}, nil
}

func (l *Logger) Log(kind string, details map[string]any) {
	if l == nil || l.f == nil {
		return
	}
	ev := Event{
		Ts:      nowUnixMs(),
		NodeID:  l.id,
		Kind:    kind,
		Details: details,
	}
	l.write(ev)
}

func (l *Logger) LogPeer(kind, peer, addr string, details map[string]any) {
	if l == nil || l.f == nil {
		return
	}
	ev := Event{
		Ts:      nowUnixMs(),
		NodeID:  l.id,
		Kind:    kind,
		Peer:    peer,
		Addr:    addr,
		Details: details,
	}
	l.write(ev)
}

func (l *Logger) write(ev Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(ev)
}

func (l *Logger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func nowUnixMs() float64 {
	return float64(time.Now().UnixNano()) / 1e6
}
