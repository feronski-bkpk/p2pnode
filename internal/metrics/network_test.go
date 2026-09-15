package metrics

import "testing"

func TestBuildNetworkReport_NotDegenerate(t *testing.T) {
	snaps := []RoutingSnapshot{
		{NodeID: "n1", Size: 2},
		{NodeID: "n2", Size: 3},
		{NodeID: "n3", Size: 2},
		{NodeID: "n4", Size: 3},
		{NodeID: "n5", Size: 2},
	}
	rep := BuildNetworkReport(4, snaps)
	if rep.Criteria.Degenerate {
		t.Fatal("must be non-degenerate")
	}
	if !rep.Criteria.Passes80PctLessThanNMinus1 {
		t.Fatal("80% check failed")
	}
	if !rep.Criteria.NoFullRegistry {
		t.Fatal("no_full_registry check failed")
	}
}

func TestBuildNetworkReport_Degenerate(t *testing.T) {
	snaps := []RoutingSnapshot{
		{NodeID: "n1", Size: 4},
		{NodeID: "n2", Size: 4},
		{NodeID: "n3", Size: 4},
		{NodeID: "n4", Size: 4},
		{NodeID: "n5", Size: 4},
	}
	rep := BuildNetworkReport(4, snaps)
	if !rep.Criteria.Degenerate {
		t.Fatal("must be degenerate (full registry)")
	}
	if rep.Criteria.NoFullRegistry {
		t.Fatal("no_full_registry should be false")
	}
}

func TestBuildNetworkReport_PartialDegenerate(t *testing.T) {
	snaps := []RoutingSnapshot{
		{NodeID: "n1", Size: 3},
		{NodeID: "n2", Size: 3},
		{NodeID: "n3", Size: 3},
		{NodeID: "n4", Size: 3},
		{NodeID: "n5", Size: 4},
	}
	rep := BuildNetworkReport(4, snaps)
	if !rep.Criteria.Passes80PctLessThanNMinus1 {
		t.Fatal("80% check should pass")
	}
	if rep.Criteria.NoFullRegistry {
		t.Fatal("no_full_registry should be false")
	}
	if !rep.Criteria.Degenerate {
		t.Fatal("must be degenerate (one node has full registry)")
	}
}
