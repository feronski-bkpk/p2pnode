package integration

import (
	"testing"
	"time"

	"p2pnode/internal/node"
)

func startClusterWithIntervals(t *testing.T, n int, basePort int,
	expire, republish time.Duration) []*node.Node {
	t.Helper()
	nodes := startCluster(t, n, basePort)
	for _, nd := range nodes {
		nd.ExpireInterval = expire
		nd.RepublishInterval = republish
		nd.StartBackgroundTasks()
	}
	return nodes
}

func TestExpireRemovesOldRecords(t *testing.T) {
	nodes := startClusterWithIntervals(t, 3, 18600, 200*time.Millisecond, time.Hour)

	rec := makeNodeRecord(t, nodes[0], 500*time.Millisecond, 1)
	_, err := nodes[0].Publish(rec, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	key := node.NodeKeyForID(rec.NodeID)

	if _, ok := nodes[0].Store.Get(key, time.Now()); !ok {
		t.Fatal("record should exist right after publish")
	}

	time.Sleep(1500 * time.Millisecond)

	if _, ok := nodes[0].Store.Get(key, time.Now()); ok {
		t.Errorf("record should be expired, but still present")
	}
}

func TestExpireDoesNotRemoveFresh(t *testing.T) {
	nodes := startClusterWithIntervals(t, 3, 18700, 200*time.Millisecond, time.Hour)

	rec := makeNodeRecord(t, nodes[0], 10*time.Second, 1)
	_, err := nodes[0].Publish(rec, 10*time.Second)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	key := node.NodeKeyForID(rec.NodeID)

	time.Sleep(700 * time.Millisecond)

	if _, ok := nodes[0].Store.Get(key, time.Now()); !ok {
		t.Errorf("fresh record should not be expired")
	}
}

func TestRepublishIncrementsSequence(t *testing.T) {
	nodes := startClusterWithIntervals(t, 3, 18800, time.Hour, 300*time.Millisecond)

	rec := makeNodeRecord(t, nodes[0], 30*time.Second, 1)
	_, err := nodes[0].Publish(rec, 30*time.Second)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	key := node.NodeKeyForID(rec.NodeID)

	initial, ok := nodes[0].Store.Get(key, time.Now())
	if !ok {
		t.Fatal("record should exist")
	}
	if initial.SequenceNumber != 1 {
		t.Fatalf("initial seq: got %d, want 1", initial.SequenceNumber)
	}

	time.Sleep(1500 * time.Millisecond)

	updated, ok := nodes[0].Store.Get(key, time.Now())
	if !ok {
		t.Fatal("record should still exist after republish")
	}
	if updated.SequenceNumber <= 1 {
		t.Errorf("sequence_number should increase, got %d", updated.SequenceNumber)
	}
	t.Logf("republished: seq %d → %d", initial.SequenceNumber, updated.SequenceNumber)
}

func TestRepublishOnlyOwnRecords(t *testing.T) {
	nodes := startCluster(t, 3, 18900)

	nodes[0].ExpireInterval = time.Hour
	nodes[0].RepublishInterval = 300 * time.Millisecond
	nodes[0].StartBackgroundTasks()

	for i := 1; i < len(nodes); i++ {
		nodes[i].ExpireInterval = time.Hour
		nodes[i].RepublishInterval = time.Hour
		nodes[i].StartBackgroundTasks()
	}

	rec := makeNodeRecord(t, nodes[1], 30*time.Second, 1)

	_, err := nodes[0].Publish(rec, 30*time.Second)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	key := node.NodeKeyForID(rec.NodeID)

	initial, ok := nodes[0].Store.Get(key, time.Now())
	if !ok {
		t.Fatal("record should exist right after publish")
	}
	if initial.SequenceNumber != 1 {
		t.Fatalf("initial seq: got %d, want 1", initial.SequenceNumber)
	}

	time.Sleep(1500 * time.Millisecond)

	updated, ok := nodes[0].Store.Get(key, time.Now())
	if !ok {
		t.Fatal("record should still exist")
	}
	if updated.SequenceNumber != 1 {
		t.Errorf("foreign record should not be republished by node 0, seq=%d",
			updated.SequenceNumber)
	}
}
