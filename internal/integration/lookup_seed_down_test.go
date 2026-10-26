package integration

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"p2pnode/internal/routing"
	"p2pnode/internal/rpc"
)

func TestLookupAfterSeedDown(t *testing.T) {
	nodes := startCluster(t, 5, 19200)

	time.Sleep(500 * time.Millisecond)

	worker := nodes[1]
	snapshotBefore := worker.Table.Snapshot()
	if len(snapshotBefore) == 0 {
		t.Fatal("worker table empty after bootstrap")
	}
	t.Logf("snapshot before: %d contacts", len(snapshotBefore))

	var target routing.ID
	for {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			t.Fatalf("rand: %v", err)
		}
		target = routing.ID(raw)
		if _, ok := worker.Table.Get(target); !ok {
			break
		}
	}
	t.Logf("target (not in table): %s", target.Short())

	ctx := context.Background()
	cfg := rpc.LookupConfig{
		Alpha:         3,
		K:             4,
		Timeout:       2 * time.Second,
		LookupTimeout: 10 * time.Second,
	}

	lookupBefore := worker.Client.LookupNode(ctx, worker.Local, worker.Table, target, cfg)
	t.Logf("lookup before: rpc=%d contacts=%d duration=%v",
		lookupBefore.RPC, len(lookupBefore.Contacts), lookupBefore.Duration)
	if lookupBefore.RPC == 0 {
		t.Fatal("lookup before: no RPC — cluster not converged")
	}

	seed := nodes[0]
	seedID := seed.Local.NodeID
	if err := seed.Stop(); err != nil {
		t.Fatalf("stop seed: %v", err)
	}
	t.Logf("seed stopped: %s", seedID.Short())
	time.Sleep(1 * time.Second)

	lookupAfter := worker.Client.LookupNode(ctx, worker.Local, worker.Table, target, cfg)
	t.Logf("lookup after: rpc=%d contacts=%d duration=%v",
		lookupAfter.RPC, len(lookupAfter.Contacts), lookupAfter.Duration)

	if lookupAfter.RPC == 0 {
		t.Fatal("lookup after: no RPC — worker can't reach anyone after seed down")
	}
	if len(lookupAfter.Contacts) == 0 {
		t.Fatal("lookup after: empty contacts")
	}
	if lookupAfter.RPC > lookupBefore.RPC*2 {
		t.Errorf("lookup after: RPC %d > 2x before %d", lookupAfter.RPC, lookupBefore.RPC)
	}
	if lookupAfter.Duration > lookupBefore.Duration*3 {
		t.Errorf("lookup after: duration %v > 3x before %v",
			lookupAfter.Duration, lookupBefore.Duration)
	}
	for _, ct := range lookupAfter.Contacts {
		if ct.NodeID == seedID {
			t.Errorf("lookup after: dead seed %s in contacts", ct.NodeID.Short())
		}
	}

	worker.Checker.Invalidate(seedID)

	removed := worker.Table.RemoveIfDead(ctx, seedID)
	if removed {
		t.Logf("RemoveIfDead(seed): removed from table")
	} else {
		t.Logf("RemoveIfDead(seed): not removed (maybe already gone?)")
	}
	if _, ok := worker.Table.Get(seedID); ok {
		t.Errorf("dead seed still in worker table after RemoveIfDead")
	}

	t.Logf("OK: worker survived seed down (before: rpc=%d, after: rpc=%d)",
		lookupBefore.RPC, lookupAfter.RPC)
}

func TestBootstrapFailsOnEmptyTable(t *testing.T) {
	n := startNode(t, t.TempDir(), 19300, []string{"127.0.0.1:19399"})
	defer n.Stop()

	err := n.Bootstrap()
	if err == nil {
		t.Fatal("Bootstrap should fail when all seeds unreachable")
	}
	t.Logf("bootstrap error (expected): %v", err)
	if n.Table.Size() != 0 {
		t.Fatalf("table should be empty, got %d", n.Table.Size())
	}
}
