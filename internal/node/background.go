package node

import (
	"context"
	"time"

	"p2pnode/internal/record"
	"p2pnode/internal/rpc"
)

const (
	ExpireInterval = 30 * time.Second

	RepublishInterval = DefaultRecordTTL / 2

	RefreshInterval = 60 * time.Second
)

func (n *Node) StartBackgroundTasks(ctx context.Context) {
	if n.bgCtx != nil {
		return
	}

	if n.ExpireInterval <= 0 {
		n.ExpireInterval = ExpireInterval
	}
	if n.RepublishInterval <= 0 {
		n.RepublishInterval = RepublishInterval
	}
	if n.RefreshInterval <= 0 {
		n.RefreshInterval = RefreshInterval
	}

	ctx, cancel := context.WithCancel(ctx)
	n.bgCtx = ctx
	n.bgCancel = cancel

	n.bgWG.Add(3)
	go func() {
		defer n.bgWG.Done()
		n.runExpireLoop(ctx)
	}()
	go func() {
		defer n.bgWG.Done()
		n.runRepublishLoop(ctx)
	}()
	go func() {
		defer n.bgWG.Done()
		n.runRefreshLoop(ctx)
	}()

	n.Log.Info("background tasks started",
		"expire_interval_sec", n.ExpireInterval.Seconds(),
		"republish_interval_sec", n.RepublishInterval.Seconds(),
		"refresh_interval_sec", n.RefreshInterval.Seconds(),
	)
}

func (n *Node) stopBackgroundTasks() {
	if n.bgCancel == nil {
		return
	}
	n.bgCancel()
	n.bgWG.Wait()
	n.bgCancel = nil
	n.bgCtx = nil
	n.Log.Info("background tasks stopped")
}

func (n *Node) runExpireLoop(ctx context.Context) {
	ticker := time.NewTicker(n.ExpireInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			removed := n.Store.Expire(now)
			if removed > 0 {
				n.Log.Info("expire: removed records",
					"count", removed,
					"store_size", n.Store.Size(),
				)
				n.Events.Log("expire", map[string]any{
					"removed":    removed,
					"store_size": n.Store.Size(),
				})
			}
		}
	}
}

func (n *Node) runRepublishLoop(ctx context.Context) {
	ticker := time.NewTicker(n.RepublishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.republishOwnRecords(ctx)
		}
	}
}

func (n *Node) republishOwnRecords(ctx context.Context) {
	now := time.Now()
	snapshot := n.Store.Snapshot()

	var localRecordID record.NodeID
	copy(localRecordID[:], n.Identity.NodeID[:])

	count := 0
	for key, sr := range snapshot {
		select {
		case <-ctx.Done():
			return
		default:
		}

		rec := sr.Record
		if rec == nil {
			continue
		}

		if rec.NodeID != localRecordID {
			continue
		}

		newRec := *rec
		newRec.SequenceNumber = rec.SequenceNumber + 1
		newRec.IssuedAt = now
		newRec.ExpiresAt = now.Add(DefaultRecordTTL)

		if err := newRec.Sign(n.Identity.PrivateKey); err != nil {
			n.Log.Warn("republish: sign failed",
				"key", key.Short(),
				"err", err,
			)
			continue
		}

		result, err := n.Publish(&newRec, DefaultRecordTTL)
		if err != nil {
			n.Log.Warn("republish: publish failed",
				"key", key.Short(),
				"err", err,
			)
			continue
		}
		count++
		n.Log.Debug("republish: done",
			"key", key.Short(),
			"seq", newRec.SequenceNumber,
			"replicas", result.Replicas,
		)
	}

	if count > 0 {
		n.Log.Info("republish: cycle done", "count", count)
		n.Events.Log("republish", map[string]any{
			"count": count,
		})
	}
}

func (n *Node) runRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(n.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.refreshTable(ctx)
		}
	}
}

func (n *Node) refreshTable(ctx context.Context) {
	if n.Table.Size() == 0 {
		return
	}
	start := time.Now()
	result := n.Client.LookupNode(ctx, n.Local, n.Table, n.Identity.NodeID, rpc.LookupConfig{
		Alpha:         n.Config.Alpha,
		K:             n.Config.KBucketSize,
		Timeout:       n.Config.RPCTimeout,
		LookupTimeout: n.Config.LookupTimeout,
	})
	n.Log.Info("refresh: done",
		"rpc", result.RPC,
		"iterations", result.Iterations,
		"table_size", n.Table.Size(),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	n.Events.Log("refresh", map[string]any{
		"rpc":         result.RPC,
		"iterations":  result.Iterations,
		"table_size":  n.Table.Size(),
		"duration_ms": time.Since(start).Milliseconds(),
	})
}
