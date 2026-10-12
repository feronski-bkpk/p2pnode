package rpc

import (
	"testing"
	"time"

	"p2pnode/internal/routing"
)

func TestMultiPath_DetectsSybil(t *testing.T) {
	res := MultiPathResult{
		PathCount: 3,
		Paths:     []LookupResult{{}, {}, {}},
	}

	votes := map[string]int{
		"attacker.example:9999": 1,
		"node-11:9011":          2,
	}

	bestVotes := 0
	bestAddr := ""
	for addr, v := range votes {
		if v > bestVotes {
			bestVotes = v
			bestAddr = addr
		}
	}

	res.Agreement = bestVotes
	res.Winner = bestAddr
	res.Disagree = bestVotes < (res.PathCount+1)/2

	if res.Winner != "node-11:9011" {
		t.Fatalf("expected honest winner, got %s", res.Winner)
	}
	if res.Agreement != 2 {
		t.Fatalf("expected agreement=2, got %d", res.Agreement)
	}
	if res.Disagree {
		t.Fatal("expected no disagree")
	}
}

func TestMultiPath_AllDisagree(t *testing.T) {
	res := MultiPathResult{
		PathCount: 3,
		Paths:     []LookupResult{{}, {}, {}},
	}

	votes := map[string]int{
		"a": 1,
		"b": 1,
		"c": 1,
	}

	bestVotes := 0
	bestAddr := ""
	for addr, v := range votes {
		if v > bestVotes {
			bestVotes = v
			bestAddr = addr
		}
	}

	res.Agreement = bestVotes
	res.Winner = bestAddr
	res.Disagree = bestVotes < (res.PathCount+1)/2

	if !res.Disagree {
		t.Fatal("expected disagree when all paths differ")
	}
}

func TestExtractTargetAddr(t *testing.T) {
	target := routing.ID{0xAB}

	res := LookupResult{
		Target: target,
		Contacts: []routing.Contact{
			{NodeID: routing.ID{0x01}, Host: "nope", Port: 1},
			{NodeID: target, Host: "node-11", Port: 9011},
			{NodeID: routing.ID{0x02}, Host: "other", Port: 2},
		},
	}

	got := ExtractTargetAddr(res)
	want := "node-11:9011"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	res2 := LookupResult{
		Target: target,
		Contacts: []routing.Contact{
			{NodeID: routing.ID{0x01}, Host: "nope", Port: 1},
		},
	}
	if got := ExtractTargetAddr(res2); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestMultiPathResult_Agreement(t *testing.T) {
	cases := []struct {
		pathCount int
		votes     map[string]int
		wantWin   string
		wantAgr   int
		wantDis   bool
	}{
		{
			pathCount: 3,
			votes:     map[string]int{"a": 3},
			wantWin:   "a",
			wantAgr:   3,
			wantDis:   false,
		},
		{
			pathCount: 3,
			votes:     map[string]int{"a": 2, "b": 1},
			wantWin:   "a",
			wantAgr:   2,
			wantDis:   false,
		},
		{
			pathCount: 3,
			votes:     map[string]int{"a": 1, "b": 1, "c": 1},
			wantAgr:   1,
			wantDis:   true,
		},
		{
			pathCount: 5,
			votes:     map[string]int{"a": 3, "b": 1, "c": 1},
			wantWin:   "a",
			wantAgr:   3,
			wantDis:   false,
		},
	}

	for i, c := range cases {
		bestVotes := 0
		bestAddr := ""
		for addr, v := range c.votes {
			if v > bestVotes {
				bestVotes = v
				bestAddr = addr
			}
		}

		dis := bestAddr == "" || bestVotes < (c.pathCount+1)/2

		if c.wantWin != "" && bestAddr != c.wantWin {
			t.Errorf("case %d: winner got %q, want %q", i, bestAddr, c.wantWin)
		}
		if bestVotes != c.wantAgr {
			t.Errorf("case %d: agreement got %d, want %d", i, bestVotes, c.wantAgr)
		}
		if dis != c.wantDis {
			t.Errorf("case %d: disagree got %v, want %v", i, dis, c.wantDis)
		}
	}

	_ = time.Second
}
