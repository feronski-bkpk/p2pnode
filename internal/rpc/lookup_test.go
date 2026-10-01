package rpc

import (
	"testing"
	"time"

	"p2pnode/internal/transport/tcp"
)

func TestLookup3Nodes(t *testing.T) {
	nodes := newTestNetwork(t, 4, 4)

	nodes[0].table.Add(nodes[1].contact, nil)

	nodes[1].table.Add(nodes[0].contact, nil)
	nodes[1].table.Add(nodes[3].contact, nil)

	nodes[3].table.Add(nodes[1].contact, nil)
	nodes[3].table.Add(nodes[2].contact, nil)

	nodes[2].table.Add(nodes[3].contact, nil)

	target := nodes[2].contact.NodeID
	if _, ok := nodes[0].table.Get(target); ok {
		t.Fatal("precondition failed: target already known to initiator")
	}

	tr := tcp.New()
	client := NewClient(tr, discardLog(), nil, testIdentity(t))

	result := client.LookupNode(
		nodes[0].contact,
		nodes[0].table,
		target,
		LookupConfig{Alpha: 3, K: 4, Timeout: 2 * time.Second},
	)

	if result.RPC < 2 {
		t.Fatalf("expected >=2 RPC (via intermediate), got %d", result.RPC)
	}
	found := false
	for _, c := range result.Contacts {
		if c.NodeID == target {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("target %s not found; got %d contacts", target.Short(), len(result.Contacts))
	}
}

func TestLookupTargetAlreadyKnown(t *testing.T) {
	nodes := newTestNetwork(t, 2, 4)
	nodes[0].table.Add(nodes[1].contact, nil)

	tr := tcp.New()
	client := NewClient(tr, discardLog(), nil, testIdentity(t))
	result := client.LookupNode(
		nodes[0].contact,
		nodes[0].table,
		nodes[1].contact.NodeID,
		LookupConfig{Alpha: 3, K: 4, Timeout: 1 * time.Second},
	)
	if len(result.Contacts) == 0 {
		t.Fatal("empty result")
	}
	if result.Contacts[0].NodeID != nodes[1].contact.NodeID {
		t.Fatal("closest contact should be the target")
	}
}

func TestLookupLogPopulated(t *testing.T) {
	nodes := newTestNetwork(t, 4, 4)

	nodes[0].table.Add(nodes[1].contact, nil)
	nodes[1].table.Add(nodes[0].contact, nil)
	nodes[1].table.Add(nodes[3].contact, nil)
	nodes[3].table.Add(nodes[1].contact, nil)
	nodes[3].table.Add(nodes[2].contact, nil)
	nodes[2].table.Add(nodes[3].contact, nil)

	tr := tcp.New()
	client := NewClient(tr, discardLog(), nil, testIdentity(t))
	result := client.LookupNode(
		nodes[0].contact,
		nodes[0].table,
		nodes[2].contact.NodeID,
		LookupConfig{Alpha: 3, K: 4, Timeout: 2 * time.Second},
	)

	if result.Log.Target == "" {
		t.Fatal("log target empty")
	}
	if result.Log.Initiator == "" {
		t.Fatal("log initiator empty")
	}
	if len(result.Log.Iterations) == 0 {
		t.Fatal("log iterations empty")
	}
	if len(result.Log.FinalContacts) == 0 {
		t.Fatal("log final contacts empty")
	}
}
