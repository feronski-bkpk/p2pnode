package metrics

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"p2pnode/internal/routing"
	"p2pnode/internal/rpc"
)

func TestSnapshotLookup(t *testing.T) {
	res := rpc.LookupResult{
		Target:     routing.ID{0xAB},
		RPC:        5,
		Iterations: 2,
		Timeouts:   1,
		Duration:   123 * time.Millisecond,
		Contacts: []routing.Contact{
			makeContact(t),
		},
		Log: rpc.LookupLog{
			Target:      "ab",
			Initiator:   "cd",
			StartUnixMs: 1000,
			EndUnixMs:   1123,
			Iterations: []rpc.LookupIteration{
				{Iter: 0, Queried: []rpc.ContactLog{{NodeID: "aa", Addr: "a"}}},
			},
			FinalContacts: []rpc.ContactLog{{NodeID: "bb", Addr: "b"}},
		},
	}
	snap := SnapshotLookup(res, true)
	if snap.RPC != 5 || snap.Iterations != 2 || snap.Timeouts != 1 {
		t.Fatal("metrics mismatch")
	}
	if !snap.TargetAbsentAtStart {
		t.Fatal("target_absent_at_start must be true")
	}
	if snap.DurationMs != 123 {
		t.Fatalf("duration_ms: %d", snap.DurationMs)
	}
	if len(snap.Target) != 64 {
		t.Fatalf("target should be 64-char hex, got %d", len(snap.Target))
	}
	if snap.Target[0:2] != "ab" {
		t.Fatalf("target should start with 'ab', got %s", snap.Target[0:2])
	}
}

func TestExportLookup(t *testing.T) {
	dir := t.TempDir()
	exp, _ := NewExporter(dir)
	res := rpc.LookupResult{
		Target: routing.ID{0x01},
		Log: rpc.LookupLog{
			Target: "01", Initiator: "02",
			StartUnixMs: 100, EndUnixMs: 200,
		},
	}
	path, err := exp.ExportLookup(res, false, "lookup.json")
	if err != nil {
		t.Fatalf("ExportLookup: %v", err)
	}
	buf, _ := os.ReadFile(path)
	var got LookupSnapshot
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Target) != 64 {
		t.Fatalf("target len: %d", len(got.Target))
	}
	if got.Target[0:2] != "01" {
		t.Fatalf("target prefix: %s", got.Target[0:2])
	}
}
