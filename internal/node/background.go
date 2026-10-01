package node

import (
	"context"
	"time"

	"p2pnode/internal/record"
)

const (
	ExpireInterval = 30 * time.Second

	RepublishInterval = DefaultRecordTTL / 2
)

func (n *Node) StartBackgroundTasks() {
	if n.bgCtx != nil {
		return
	}

	if n.ExpireInterval <= 0 {
		n.ExpireInterval = ExpireInterval
	}
	if n.RepublishInterval <= 0 {
		n.RepublishInterval = RepublishInterval
	}

	ctx, cancel := context.WithCancel(context.Background())
	n.bgCtx = ctx
	n.bgCancel = cancel

	n.bgWG.Add(2)
	go func() {
		defer n.bgWG.Done()
		n.runExpireLoop(ctx)
	}()
	go func() {
		defer n.bgWG.Done()
		n.runRepublishLoop(ctx)
	}()

	n.Log.Info("background tasks started",
		"expire_interval_sec", n.ExpireInterval.Seconds(),
		"republish_interval_sec", n.RepublishInterval.Seconds(),
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
