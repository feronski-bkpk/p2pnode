package metrics

import (
	"time"

	"p2pnode/internal/rpc"
)

type LookupSnapshot struct {
	Target              string         `json:"target"`
	Initiator           string         `json:"initiator"`
	StartUnixMs         int64          `json:"start_unix_ms"`
	EndUnixMs           int64          `json:"end_unix_ms"`
	DurationMs          int64          `json:"duration_ms"`
	RPC                 int            `json:"rpc"`
	Iterations          int            `json:"iterations"`
	Timeouts            int            `json:"timeouts"`
	FinalContacts       []ContactInfo  `json:"final_contacts"`
	IterationsLog       []IterSnapshot `json:"iterations_log"`
	TargetAbsentAtStart bool           `json:"target_absent_at_start"`
}

type ContactInfo struct {
	NodeID string `json:"node_id"`
	Addr   string `json:"addr"`
}

type IterSnapshot struct {
	Iter     int           `json:"iter"`
	Queried  []ContactInfo `json:"queried"`
	Found    []ContactInfo `json:"found"`
	Timeouts []ContactInfo `json:"timeouts"`
}

func SnapshotLookup(res rpc.LookupResult, targetAbsentAtStart bool) LookupSnapshot {
	snap := LookupSnapshot{
		Target:              res.Target.String(),
		Initiator:           res.Log.Initiator,
		StartUnixMs:         res.Log.StartUnixMs,
		EndUnixMs:           res.Log.EndUnixMs,
		DurationMs:          res.Duration.Milliseconds(),
		RPC:                 res.RPC,
		Iterations:          res.Iterations,
		Timeouts:            res.Timeouts,
		TargetAbsentAtStart: targetAbsentAtStart,
		FinalContacts:       []ContactInfo{},
		IterationsLog:       []IterSnapshot{},
	}
	for _, c := range res.Contacts {
		snap.FinalContacts = append(snap.FinalContacts, ContactInfo{
			NodeID: c.NodeID.String(),
			Addr:   c.Addr(),
		})
	}
	for _, it := range res.Log.Iterations {
		is := IterSnapshot{
			Iter:     it.Iter,
			Queried:  []ContactInfo{},
			Found:    []ContactInfo{},
			Timeouts: []ContactInfo{},
		}
		for _, c := range it.Queried {
			is.Queried = append(is.Queried, ContactInfo{NodeID: c.NodeID, Addr: c.Addr})
		}
		for _, c := range it.Found {
			is.Found = append(is.Found, ContactInfo{NodeID: c.NodeID, Addr: c.Addr})
		}
		for _, c := range it.Timeouts {
			is.Timeouts = append(is.Timeouts, ContactInfo{NodeID: c.NodeID, Addr: c.Addr})
		}
		snap.IterationsLog = append(snap.IterationsLog, is)
	}
	return snap
}

func (e *Exporter) ExportLookup(res rpc.LookupResult, targetAbsentAtStart bool, name string) (string, error) {
	snap := SnapshotLookup(res, targetAbsentAtStart)
	return e.WriteJSON(name, snap)
}

func (e *Exporter) ExportLookupWithTimestamp(res rpc.LookupResult, targetAbsentAtStart bool) (string, error) {
	snap := SnapshotLookup(res, targetAbsentAtStart)
	short := res.Target.String()
	if len(short) > 8 {
		short = short[:8]
	}
	return e.WriteJSONWithTimestamp("lookup-"+short, snap)
}

func nowMs() int64 { return time.Now().UnixMilli() }
