package tunnel

import (
	"context"
	"time"
)

func StartStoreExpireLoop(
	ctx context.Context,
	relayStore *RelayStore,
	destStore *DestSessionStore,
	log Logger,
) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if r := relayStore.ExpireAll(); r > 0 && log != nil {
					log.Info("tunnel: expired relay states",
						"removed", r,
						"remaining", relayStore.Count())
				}
				if r := destStore.ExpireAll(); r > 0 && log != nil {
					log.Info("tunnel: expired dest sessions",
						"removed", r,
						"remaining", destStore.Count())
				}
			}
		}
	}()
}
