package metrics

import (
	"p2pnode/internal/routing"
)

type ContactSnapshot struct {
	NodeID         string `json:"node_id"`
	Addr           string `json:"addr"`
	PubKeyHex      string `json:"pubkey_hex"`
	LastSeenMs     uint64 `json:"last_seen_ms"`
	LastVerifiedMs uint64 `json:"last_verified_ms"`
}

type BucketSnapshot struct {
	Index    int               `json:"index"`
	Contacts []ContactSnapshot `json:"contacts"`
}

type RoutingSnapshot struct {
	NodeID      string           `json:"node_id"`
	K           int              `json:"k"`
	Size        int              `json:"size"`
	BucketCount int              `json:"bucket_count"`
	MaxFill     int              `json:"max_bucket_fill"`
	MinFill     int              `json:"min_bucket_fill"`
	Buckets     []BucketSnapshot `json:"buckets"`
	GeneratedAt int64            `json:"generated_at_ms"`
}

func SnapshotRouting(self routing.ID, table *routing.RoutingTable) RoutingSnapshot {
	buckets := table.SnapshotBuckets()

	snap := RoutingSnapshot{
		NodeID:      self.String(),
		K:           table.K(),
		Size:        table.Size(),
		BucketCount: len(buckets),
		GeneratedAt: nowMs(),
	}

	maxFill := 0
	minFill := -1
	for _, b := range buckets {
		bs := BucketSnapshot{
			Index:    b.Index,
			Contacts: make([]ContactSnapshot, 0, len(b.Contacts)),
		}
		for _, c := range b.Contacts {
			bs.Contacts = append(bs.Contacts, ContactSnapshot{
				NodeID:         c.NodeID.String(),
				Addr:           c.Addr(),
				PubKeyHex:      hexEncode(c.IdentityPublicKey),
				LastSeenMs:     c.LastSeenMs,
				LastVerifiedMs: c.LastVerifiedMs,
			})
		}
		snap.Buckets = append(snap.Buckets, bs)

		if n := len(b.Contacts); n > maxFill {
			maxFill = n
		}
		if minFill < 0 || len(b.Contacts) < minFill {
			minFill = len(b.Contacts)
		}
	}
	if minFill < 0 {
		minFill = 0
	}
	snap.MaxFill = maxFill
	snap.MinFill = minFill
	return snap
}

func (e *Exporter) ExportRouting(self routing.ID, table *routing.RoutingTable, name string) (string, error) {
	snap := SnapshotRouting(self, table)
	return e.WriteJSON(name, snap)
}

func (e *Exporter) ExportRoutingWithTimestamp(self routing.ID, table *routing.RoutingTable) (string, error) {
	snap := SnapshotRouting(self, table)
	short := self.String()
	if len(short) > 8 {
		short = short[:8]
	}
	return e.WriteJSONWithTimestamp("routing-"+short, snap)
}

func hexEncode(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, x := range b {
		out[i*2] = hexdigits[x>>4]
		out[i*2+1] = hexdigits[x&0x0F]
	}
	return string(out)
}
