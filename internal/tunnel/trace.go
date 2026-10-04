package tunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type TraceEvent struct {
	Time      time.Time      `json:"time"`
	Kind      string         `json:"kind"`
	TunnelID  string         `json:"tunnel_id,omitempty"`
	MessageID string         `json:"message_id,omitempty"`
	HopIndex  int            `json:"hop_index,omitempty"`
	From      string         `json:"from,omitempty"`
	To        string         `json:"to,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

const (
	TraceKindRouteSelected = "route_selected"
	TraceKindRouteBuilt    = "route_built"
	TraceKindBuildStart    = "build_start"
	TraceKindBuildAck      = "build_ack"
	TraceKindBuildOK       = "build_ok"
	TraceKindBuildFail     = "build_fail"
	TraceKindRelayBuild    = "relay_build"
	TraceKindRelayAck      = "relay_ack"
	TraceKindDestReady     = "dest_ready"
	TraceKindSendStart     = "send_start"
	TraceKindSendAck       = "send_ack"
	TraceKindSendFail      = "send_fail"
	TraceKindDataRecv      = "data_recv"
	TraceKindSessionClosed = "session_closed"
	TraceKindPoolBuilt     = "pool_built"
	TraceKindRebuild       = "rebuild"
)

type Trace struct {
	mu      sync.Mutex
	events  []TraceEvent
	maxSize int
}

func NewTrace(maxSize int) *Trace {
	if maxSize <= 0 {
		maxSize = 5000
	}
	return &Trace{
		events:  make([]TraceEvent, 0, maxSize),
		maxSize: maxSize,
	}
}

func (t *Trace) Add(ev TraceEvent) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	t.events = append(t.events, ev)
	if len(t.events) > t.maxSize {
		t.events = t.events[len(t.events)-t.maxSize:]
	}
}

func (t *Trace) Snapshot() []TraceEvent {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TraceEvent, len(t.events))
	copy(out, t.events)
	return out
}

func (t *Trace) Count() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.events)
}

func (t *Trace) ExportJSON(path string) error {
	if t == nil {
		return fmt.Errorf("tunnel: trace is nil")
	}
	events := t.Snapshot()
	data := struct {
		Events []TraceEvent `json:"events"`
		Count  int          `json:"count"`
	}{
		Events: events,
		Count:  len(events),
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("tunnel: trace marshal: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("tunnel: trace write: %w", err)
	}
	return nil
}

func ShortID(id [32]byte) string {
	return fmt.Sprintf("%x", id[:4])
}
