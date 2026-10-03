package tunnel

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

type RouteSource interface {
	FindDest(destID [32]byte) (addr string, pubkey []byte, err error)
	KnownPeers() [][32]byte
	AddrOf(nodeID [32]byte) (string, bool)
	Ping(addr string) (time.Duration, error)
}

type RouteBuilder struct {
	LocalID   [32]byte
	LocalAddr string
	MaxHops   uint8

	Source   RouteSource
	Profiles *ProfileStore

	Log Logger

	PingTimeout time.Duration
}

func NewRouteBuilder(localID [32]byte, localAddr string, maxHops uint8,
	src RouteSource, profiles *ProfileStore) *RouteBuilder {
	if maxHops == 0 {
		maxHops = 3
	}
	return &RouteBuilder{
		LocalID:     localID,
		LocalAddr:   localAddr,
		MaxHops:     maxHops,
		Source:      src,
		Profiles:    profiles,
		PingTimeout: 3 * time.Second,
	}
}

func (rb *RouteBuilder) Build(destID [32]byte, exclude [][32]byte) ([]Hop, error) {
	destAddr, _, err := rb.Source.FindDest(destID)
	if err != nil {
		rb.logf("route: dest not found",
			"dest", fmt.Sprintf("%x", destID[:4]),
			"err", err)
		return nil, fmt.Errorf("tunnel: find dest: %w", err)
	}

	candidates := rb.selectCandidates(destID, exclude)
	if len(candidates) < int(rb.MaxHops) {
		rb.logf("route: not enough candidates",
			"dest", fmt.Sprintf("%x", destID[:4]),
			"candidates", len(candidates),
			"excluded", len(exclude),
			"need", rb.MaxHops)
		return nil, fmt.Errorf("%w: only %d candidates, need %d",
			ErrNoCandidates, len(candidates), rb.MaxHops)
	}

	rb.logf("route: candidates collected",
		"dest", fmt.Sprintf("%x", destID[:4]),
		"total", len(candidates),
		"excluded", len(exclude),
		"max_hops", rb.MaxHops)

	ranked := rb.Profiles.Rank(candidates)

	topN := 5
	if len(ranked) < topN {
		topN = len(ranked)
	}
	for i := 0; i < topN; i++ {
		rb.logCandidate("route: candidate ranked", i, ranked[i])
	}

	poolSize := int(rb.MaxHops) * 2
	if poolSize > len(ranked) {
		poolSize = len(ranked)
	}
	pool := ranked[:poolSize]
	shuffleIDs(pool)

	relays := make([]Hop, 0, rb.MaxHops)
	used := map[[32]byte]bool{
		rb.LocalID: true,
		destID:     true,
	}
	for _, id := range exclude {
		used[id] = true
	}

	for _, id := range pool {
		if used[id] {
			rb.logReject(id, "already_used", "")
			continue
		}
		addr, ok := rb.Source.AddrOf(id)
		if !ok {
			rb.logReject(id, "no_addr", "")
			continue
		}

		start := time.Now()
		_, err := rb.Source.Ping(addr)
		rtt := time.Since(start)

		if err != nil {
			rb.Profiles.RecordFailure(id)
			rb.logReject(id, "ping_failed", addr)
			continue
		}
		rb.Profiles.RecordSuccess(id, rtt)

		if uint8(len(relays)) >= rb.MaxHops {
			rb.logReject(id, "enough_relays", addr)
			continue
		}

		relays = append(relays, Hop{NodeID: id, Addr: addr, Type: HopRelay})
		used[id] = true
		rb.logAccept(len(relays)-1, id, addr, rtt)
	}

	if uint8(len(relays)) < rb.MaxHops {
		rb.logf("route: not enough alive relays",
			"dest", fmt.Sprintf("%x", destID[:4]),
			"alive", len(relays),
			"need", rb.MaxHops)
		return nil, fmt.Errorf("%w: only %d alive relays, need %d",
			ErrNoCandidates, len(relays), rb.MaxHops)
	}

	path := make([]Hop, 0, len(relays)+2)
	path = append(path, Hop{NodeID: rb.LocalID, Addr: rb.LocalAddr, Type: HopInitiator})
	path = append(path, relays...)
	path = append(path, Hop{NodeID: destID, Addr: destAddr, Type: HopDest})

	hops := make([]string, 0, len(relays))
	for _, r := range relays {
		hops = append(hops, fmt.Sprintf("%x@%s", r.NodeID[:4], r.Addr))
	}
	rb.logf("route: built",
		"dest", fmt.Sprintf("%x", destID[:4]),
		"relays", len(relays),
		"path", hops)

	return path, nil
}

func (rb *RouteBuilder) logf(msg string, args ...any) {
	if rb.Log != nil {
		rb.Log.Info(msg, args...)
	}
}

func (rb *RouteBuilder) logCandidate(msg string, rank int, id [32]byte) {
	if rb.Log == nil {
		return
	}
	p, ok := rb.Profiles.Get(id)
	if !ok {
		rb.Log.Debug(msg,
			"rank", rank,
			"node_id", fmt.Sprintf("%x", id[:4]),
			"profile", "none")
		return
	}
	rb.Log.Debug(msg,
		"rank", rank,
		"node_id", fmt.Sprintf("%x", id[:4]),
		"successes", p.Successes,
		"failures", p.Failures,
		"success_ema", fmt.Sprintf("%.3f", p.SuccessEMA),
		"latency_ema", p.LatencyEMA.String(),
		"consecutive_failures", p.ConsecutiveFailures)
}

func (rb *RouteBuilder) logReject(id [32]byte, reason, addr string) {
	if rb.Log == nil {
		return
	}
	args := []any{
		"node_id", fmt.Sprintf("%x", id[:4]),
		"reason", reason,
	}
	if addr != "" {
		args = append(args, "addr", addr)
	}
	if p, ok := rb.Profiles.Get(id); ok {
		args = append(args,
			"success_ema", fmt.Sprintf("%.3f", p.SuccessEMA),
			"failures", p.Failures,
		)
	}
	rb.Log.Debug("route: candidate rejected", args...)
}

func (rb *RouteBuilder) logAccept(hopIndex int, id [32]byte, addr string, rtt time.Duration) {
	if rb.Log == nil {
		return
	}
	args := []any{
		"hop_index", hopIndex,
		"node_id", fmt.Sprintf("%x", id[:4]),
		"addr", addr,
		"rtt", rtt.String(),
	}
	if p, ok := rb.Profiles.Get(id); ok {
		args = append(args,
			"success_ema", fmt.Sprintf("%.3f", p.SuccessEMA),
			"latency_ema", p.LatencyEMA.String(),
		)
	}
	rb.Log.Info("route: candidate accepted", args...)
}

func (rb *RouteBuilder) selectCandidates(destID [32]byte, exclude [][32]byte) [][32]byte {
	excl := make(map[[32]byte]bool, len(exclude))
	for _, id := range exclude {
		excl[id] = true
	}

	peers := rb.Source.KnownPeers()
	out := make([][32]byte, 0, len(peers))
	for _, id := range peers {
		if id == rb.LocalID {
			continue
		}
		if id == destID {
			continue
		}
		if excl[id] {
			continue
		}
		out = append(out, id)
	}
	return out
}

func shuffleIDs(ids [][32]byte) {
	for i := len(ids) - 1; i > 0; i-- {
		jBig, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return
		}
		j := int(jBig.Int64())
		ids[i], ids[j] = ids[j], ids[i]
	}
}
