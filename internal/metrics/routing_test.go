package metrics

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"p2pnode/internal/identity"
	"p2pnode/internal/routing"
)

func makeContact(t *testing.T) routing.Contact {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return routing.Contact{
		NodeID:            identity.NodeIDFromPublicKey(pub),
		IdentityAlgorithm: "ed25519",
		IdentityPublicKey: pub,
		Host:              "127.0.0.1",
		Port:              9001,
	}
}

func TestSnapshotRouting(t *testing.T) {
	self := makeContact(t).NodeID
	table := routing.NewRoutingTable(self, 4)
	for i := 0; i < 3; i++ {
		table.Add(makeContact(t), nil)
	}

	snap := SnapshotRouting(self, table)
	if snap.NodeID != self.String() {
		t.Fatal("node_id mismatch")
	}
	if snap.K != 4 {
		t.Fatalf("k: %d", snap.K)
	}
	if snap.Size != 3 {
		t.Fatalf("size: %d", snap.Size)
	}
	if snap.BucketCount == 0 {
		t.Fatal("no buckets")
	}
}

func TestExportRouting(t *testing.T) {
	dir := t.TempDir()
	exp, err := NewExporter(dir)
	if err != nil {
		t.Fatalf("NewExporter: %v", err)
	}
	self := makeContact(t).NodeID
	table := routing.NewRoutingTable(self, 4)
	table.Add(makeContact(t), nil)

	path, err := exp.ExportRouting(self, table, "routing.json")
	if err != nil {
		t.Fatalf("ExportRouting: %v", err)
	}

	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got RoutingSnapshot
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Size != 1 {
		t.Fatalf("size: %d", got.Size)
	}
	if filepath.Base(path) != "routing.json" {
		t.Fatalf("path: %s", path)
	}
}

func TestSnapshotRouting_EmptyTable(t *testing.T) {
	self := makeContact(t).NodeID
	table := routing.NewRoutingTable(self, 4)
	snap := SnapshotRouting(self, table)
	if snap.Size != 0 {
		t.Fatalf("size: %d", snap.Size)
	}
	if snap.BucketCount != 0 {
		t.Fatalf("bucket_count: %d", snap.BucketCount)
	}
}
