package node

import (
	"p2pnode/internal/events"
	"p2pnode/internal/routing"
)

type routingObserver struct {
	ev *events.Logger
}

func NewRoutingObserver(ev *events.Logger) routing.Observer {
	if ev == nil {
		return nil
	}
	return &routingObserver{ev: ev}
}

func (o *routingObserver) ContactAdded(peer routing.ID, addr string, bucketIdx int) {
	o.ev.LogPeer("contact_added", peer.Short(), addr, map[string]any{
		"bucket": bucketIdx,
	})
}

func (o *routingObserver) ContactUpdated(peer routing.ID, addr string, bucketIdx int) {
	o.ev.LogPeer("contact_updated", peer.Short(), addr, map[string]any{
		"bucket": bucketIdx,
	})
}

func (o *routingObserver) ContactRemoved(peer routing.ID, addr string, reason string) {
	o.ev.LogPeer("contact_removed", peer.Short(), addr, map[string]any{
		"reason": reason,
	})
}
