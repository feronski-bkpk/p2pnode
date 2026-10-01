package integration

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"p2pnode/internal/node"
	"p2pnode/internal/record"
	"p2pnode/internal/store"
)

func makeNodeRecord(t *testing.T, n *node.Node, ttl time.Duration, seq uint64) *record.NodeRecord {
	t.Helper()

	priv := n.Identity.PrivateKey
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("bad private key size: %d", len(priv))
	}

	addr := n.Listener.Addr()
	rec, err := record.New(priv, []string{addr}, ttl, seq)
	if err != nil {
		t.Fatalf("record.New: %v", err)
	}
	return rec
}

func TestPublishAndFindValue(t *testing.T) {
	nodes := startCluster(t, 5, 18100)

	for i, n := range nodes {
		t.Logf("node %d: table_size=%d store_size=%d", i, n.Table.Size(), n.Store.Size())
	}

	rec := makeNodeRecord(t, nodes[0], 3*time.Minute, 1)

	result, err := nodes[1].Publish(rec, 3*time.Minute)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	t.Logf("publish: replicas=%d local=%v duration=%v",
		result.Replicas, result.LocalStored, result.Duration)

	if result.Replicas < 2 {
		t.Errorf("expected >=2 replicas, got %d", result.Replicas)
	}

	key := node.NodeKeyForID(rec.NodeID)

	time.Sleep(100 * time.Millisecond)
	found, err := nodes[3].FindValue(key)
	if err != nil {
		t.Fatalf("FindValue: %v", err)
	}
	if found.NodeID != rec.NodeID {
		t.Fatalf("found wrong node: got %s, want %s",
			found.NodeID.Short(), rec.NodeID.Short())
	}
	if found.SequenceNumber != rec.SequenceNumber {
		t.Fatalf("sequence_number mismatch: got %d, want %d",
			found.SequenceNumber, rec.SequenceNumber)
	}
}

func TestFindValueLocalHit(t *testing.T) {
	nodes := startCluster(t, 3, 18200)

	rec := makeNodeRecord(t, nodes[0], 3*time.Minute, 1)
	_, err := nodes[0].Publish(rec, 3*time.Minute)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	key := node.NodeKeyForID(rec.NodeID)
	found, err := nodes[0].FindValue(key)
	if err != nil {
		t.Fatalf("FindValue: %v", err)
	}
	if found.NodeID != rec.NodeID {
		t.Fatal("wrong node")
	}
}

func TestFindValueNotFound(t *testing.T) {
	nodes := startCluster(t, 3, 18300)

	var key store.ID
	for i := range key {
		key[i] = 0xAA
	}

	_, err := nodes[0].FindValue(key)
	if !errors.Is(err, node.ErrValueNotFound) {
		t.Fatalf("want ErrValueNotFound, got %v", err)
	}
}

func TestAntiRollback(t *testing.T) {
	nodes := startCluster(t, 3, 18400)

	rec10 := makeNodeRecord(t, nodes[0], 3*time.Minute, 10)
	if _, err := nodes[0].Publish(rec10, 3*time.Minute); err != nil {
		t.Fatalf("Publish rec10: %v", err)
	}

	rec5 := makeNodeRecord(t, nodes[0], 3*time.Minute, 5)
	_, err := nodes[0].Publish(rec5, 3*time.Minute)

	if err == nil {
		t.Logf("Publish rec5 didn't return error (may be ok if remote took priority)")
	}

	key := node.NodeKeyForID(rec10.NodeID)
	found, err := nodes[0].FindValue(key)
	if err != nil {
		t.Fatalf("FindValue: %v", err)
	}
	if found.SequenceNumber != 10 {
		t.Fatalf("sequence_number: got %d, want 10", found.SequenceNumber)
	}
}

func TestFindValueAfterNodeShutdown(t *testing.T) {
	nodes := startCluster(t, 5, 18500)

	rec := makeNodeRecord(t, nodes[0], 3*time.Minute, 1)
	result, err := nodes[0].Publish(rec, 3*time.Minute)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result.Replicas < 2 {
		t.Fatalf("expected >=2 replicas, got %d", result.Replicas)
	}
	t.Logf("published to %d nodes", result.Replicas)

	key := node.NodeKeyForID(rec.NodeID)

	_ = nodes[1].Stop()
	time.Sleep(100 * time.Millisecond)

	found, err := nodes[3].FindValue(key)
	if err != nil {
		t.Fatalf("FindValue after shutdown: %v", err)
	}
	if found.NodeID != rec.NodeID {
		t.Fatal("wrong node")
	}
}
