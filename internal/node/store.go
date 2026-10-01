package node

import (
	"errors"
	"fmt"
	"time"

	"p2pnode/internal/record"
	"p2pnode/internal/routing"
	"p2pnode/internal/rpc"
	"p2pnode/internal/store"
)

const DefaultReplication = 3

const DefaultRecordTTL = 180 * time.Second

var (
	ErrNotEnoughReplicas = errors.New("node: not enough replicas confirmed")
	ErrValueNotFound     = errors.New("node: value not found")
)

type StoreResult struct {
	Key           store.ID
	Replicas      int
	SelectedNodes []routing.Contact
	LocalStored   bool
	Duration      time.Duration
}

func (n *Node) Publish(rec *record.NodeRecord, ttl time.Duration) (*StoreResult, error) {
	start := time.Now()

	if err := rec.Validate(time.Now()); err != nil {
		return nil, fmt.Errorf("node: validate: %w", err)
	}

	if ttl <= 0 {
		ttl = DefaultRecordTTL
	}

	value, err := rec.Encode()
	if err != nil {
		return nil, fmt.Errorf("node: encode: %w", err)
	}

	nodeKey := NodeKeyForID(rec.NodeID)

	n.Log.Info("publish: starting",
		"key", nodeKey.Short(),
		"node_id", rec.NodeID.Short(),
		"alias", rec.Alias,
		"seq", rec.SequenceNumber,
		"ttl_sec", int(ttl.Seconds()),
	)

	n.Events.Log("publish_start", map[string]any{
		"key":     nodeKey.Short(),
		"node_id": rec.NodeID.Short(),
		"alias":   rec.Alias,
		"seq":     rec.SequenceNumber,
		"ttl_sec": int(ttl.Seconds()),
	})

	result, err := n.publishToKey(nodeKey, rec, value, ttl, start)
	if err != nil {
		return result, err
	}

	if rec.Alias != "" {
		aliasKey := AliasKeyForName(rec.Alias)
		n.Log.Info("publish: alias key", "alias", rec.Alias, "key", aliasKey.Short())

		aliasResult, aliasErr := n.publishToKey(aliasKey, rec, value, ttl, start)
		if aliasErr != nil {
			n.Log.Warn("publish: alias failed", "err", aliasErr)
		} else {
			n.Log.Info("publish: alias done",
				"alias", rec.Alias,
				"key", aliasKey.Short(),
				"replicas", aliasResult.Replicas,
				"local", aliasResult.LocalStored,
			)
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (n *Node) publishToKey(key store.ID, rec *record.NodeRecord, value []byte, ttl time.Duration, start time.Time) (*StoreResult, error) {
	target := routing.ID(key)
	candidates := n.findClosestNodes(target, DefaultReplication+1)

	if len(candidates) == 0 {
		inserted, err := n.Store.Put(key, rec, time.Now(), ttl)
		if err != nil {
			return nil, fmt.Errorf("node: local store: %w", err)
		}
		return &StoreResult{
			Key:         key,
			Replicas:    0,
			LocalStored: inserted,
			Duration:    time.Since(start),
		}, nil
	}

	result := &StoreResult{
		Key:           key,
		SelectedNodes: make([]routing.Contact, 0, DefaultReplication),
	}

	type reply struct {
		node routing.Contact
		ok   bool
		err  error
	}

	limit := min(DefaultReplication, len(candidates))
	ch := make(chan reply, limit)
	for _, c := range candidates[:limit] {
		go func(c routing.Contact) {
			ok, err := n.Client.StoreRPC(
				n.Local, c, key, value, ttl, n.Config.PingTimeout,
			)
			ch <- reply{node: c, ok: ok, err: err}
		}(c)
	}

	for i := 0; i < limit; i++ {
		r := <-ch
		if r.err != nil {
			n.Log.Debug("publish: replica failed",
				"peer", r.node.NodeID.Short(),
				"err", r.err)
			continue
		}
		if r.ok {
			result.Replicas++
			result.SelectedNodes = append(result.SelectedNodes, r.node)
		}
	}

	inserted, err := n.Store.Put(key, rec, time.Now(), ttl)
	if err != nil {
		n.Log.Warn("publish: local put failed", "err", err)
	} else {
		result.LocalStored = inserted
	}

	return result, nil
}

func (n *Node) FindValue(key store.ID) (*record.NodeRecord, error) {
	now := time.Now()

	if rec, ok := n.Store.Get(key, now); ok {
		n.Events.Log("findvalue_local_hit", map[string]any{
			"key": key.Short(),
		})
		return rec, nil
	}

	n.Events.Log("findvalue_start", map[string]any{
		"key": key.Short(),
	})

	target := routing.ID(key)
	shortlist := n.Table.Closest(target, n.Config.KBucketSize)
	if len(shortlist) == 0 {
		return nil, ErrValueNotFound
	}

	queried := make(map[routing.ID]bool)
	allCandidates := shortlist

	for iter := 0; iter < routing.IDBits; iter++ {
		var toQuery []routing.Contact
		for _, c := range allCandidates {
			if queried[c.NodeID] {
				continue
			}
			if c.NodeID == n.Local.NodeID {
				continue
			}
			toQuery = append(toQuery, c)
			queried[c.NodeID] = true
			if len(toQuery) >= n.Config.Alpha {
				break
			}
		}
		if len(toQuery) == 0 {
			break
		}

		type reply struct {
			from  routing.Contact
			value []byte
			found bool
			nodes []routing.Contact
			err   error
		}
		ch := make(chan reply, len(toQuery))
		for _, c := range toQuery {
			go func(c routing.Contact) {
				value, found, nodes, err := n.Client.FindValueRPC(
					n.Local, c, key, n.Config.PingTimeout,
				)
				ch <- reply{from: c, value: value, found: found, nodes: nodes, err: err}
			}(c)
		}

		var newCandidates []routing.Contact
		var foundValue []byte
		for i := 0; i < len(toQuery); i++ {
			r := <-ch
			if r.err != nil {
				n.Table.Remove(r.from.NodeID)
				continue
			}
			r.from.MarkVerified()
			n.Table.Add(r.from, nil)
			if r.found {
				foundValue = r.value
				break
			}
			newCandidates = append(newCandidates, r.nodes...)
		}

		if foundValue != nil {
			rec, err := record.Decode(foundValue)
			if err != nil {
				return nil, fmt.Errorf("node: decode found value: %w", err)
			}
			if err := rec.Validate(time.Now()); err != nil {
				return nil, fmt.Errorf("node: validate found value: %w", err)
			}

			_, _ = n.Store.Put(key, rec, time.Now(), DefaultRecordTTL)

			n.Events.Log("findvalue_found", map[string]any{
				"key": key.Short(),
				"rpc": iter + 1,
			})
			return rec, nil
		}

		for _, c := range newCandidates {
			if c.NodeID == n.Local.NodeID {
				continue
			}
			allCandidates = append(allCandidates, c)
			n.Table.Add(c, nil)
		}
	}

	n.Events.Log("findvalue_notfound", map[string]any{
		"key": key.Short(),
	})
	return nil, ErrValueNotFound
}

func (n *Node) findClosestNodes(target routing.ID, n_ int) []routing.Contact {
	n.Log.Info("findClosestNodes: enter",
		"target", target.Short(),
		"table_size", n.Table.Size(),
	)
	result := n.Client.LookupNode(
		n.Local,
		n.Table,
		target,
		rpc.LookupConfig{
			Alpha:   n.Config.Alpha,
			K:       n.Config.KBucketSize,
			Timeout: n.Config.PingTimeout,
		},
	)
	n.Log.Info("findClosestNodes: result",
		"target", target.Short(),
		"rpc", result.RPC,
		"iterations", result.Iterations,
		"timeouts", result.Timeouts,
		"contacts", len(result.Contacts),
	)
	if len(result.Contacts) > n_ {
		return result.Contacts[:n_]
	}
	return result.Contacts
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
