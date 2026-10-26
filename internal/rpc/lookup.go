package rpc

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"
	"time"

	"p2pnode/internal/routing"
)

type LookupConfig struct {
	Alpha         int
	K             int
	Timeout       time.Duration
	LookupTimeout time.Duration
}

type LookupResult struct {
	Target   routing.ID
	Contacts []routing.Contact

	RPC        int
	Iterations int
	Timeouts   int
	Duration   time.Duration

	Log LookupLog
}

type LookupLog struct {
	Target        string            `json:"target"`
	Initiator     string            `json:"initiator"`
	StartUnixMs   int64             `json:"start_unix_ms"`
	EndUnixMs     int64             `json:"end_unix_ms"`
	Iterations    []LookupIteration `json:"iterations"`
	FinalContacts []ContactLog      `json:"final_contacts"`
}

type LookupIteration struct {
	Iter     int          `json:"iter"`
	Queried  []ContactLog `json:"queried"`
	Found    []ContactLog `json:"found"`
	Timeouts []ContactLog `json:"timeouts"`
	Dead     []ContactLog `json:"dead,omitempty"`
}

type ContactLog struct {
	NodeID string `json:"node_id"`
	Addr   string `json:"addr"`
}

type candidateState int

const (
	stateUnqueried candidateState = iota
	stateAlive
	stateDead
)

type MultiPathResult struct {
	Target       routing.ID
	Paths        []LookupResult
	PathCount    int
	Agreement    int
	Winner       string
	WinnerNodeID routing.ID
	Disagree     bool
	Duration     time.Duration
}

func (c *Client) LookupNode(
	ctx context.Context,
	local routing.Contact,
	table *routing.RoutingTable,
	target routing.ID,
	cfg LookupConfig,
) LookupResult {
	lookupTimeout := cfg.LookupTimeout
	if lookupTimeout <= 0 {
		lookupTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	start := time.Now()
	result := LookupResult{
		Target: target,
		Log: LookupLog{
			Target:      target.String(),
			Initiator:   local.NodeID.String(),
			StartUnixMs: start.UnixMilli(),
		},
	}

	c.ev.Log("lookup_start", map[string]any{
		"target":    target.Short(),
		"initiator": local.NodeID.Short(),
	})

	shortlist := table.Closest(target, cfg.K)

	states := make(map[routing.ID]candidateState, len(shortlist))
	for _, ct := range shortlist {
		states[ct.NodeID] = stateUnqueried
	}
	inFlight := make(map[routing.ID]bool)

	for iter := 0; iter < routing.IDBits; iter++ {
		if err := ctx.Err(); err != nil {
			c.ev.Log("lookup_ctx_done", map[string]any{
				"lookup_target": target.Short(),
				"err":           err.Error(),
			})
			break
		}

		var toQuery []routing.Contact
		for _, ct := range shortlist {
			if states[ct.NodeID] != stateUnqueried {
				continue
			}
			if inFlight[ct.NodeID] {
				continue
			}
			if ct.NodeID == local.NodeID {
				continue
			}
			toQuery = append(toQuery, ct)
			if len(toQuery) >= cfg.Alpha {
				break
			}
		}
		if len(toQuery) == 0 {
			break
		}

		for _, ct := range toQuery {
			inFlight[ct.NodeID] = true
		}

		iterLog := LookupIteration{Iter: iter}
		queriedShort := make([]string, 0, len(toQuery))
		for _, ct := range toQuery {
			iterLog.Queried = append(iterLog.Queried, contactLog(ct))
			queriedShort = append(queriedShort, ct.NodeID.Short())
		}

		type reply struct {
			from     routing.Contact
			contacts []routing.Contact
			err      error
		}
		ch := make(chan reply, len(toQuery))
		var wg sync.WaitGroup
		for _, ct := range toQuery {
			wg.Add(1)
			go func(ct routing.Contact) {
				defer wg.Done()
				contacts, err := c.FindNodeRPC(ctx, local, ct, target, cfg.Timeout)
				ch <- reply{from: ct, contacts: contacts, err: err}
			}(ct)
		}
		wg.Wait()
		close(ch)

		var newContacts []routing.Contact
		foundTarget := false
		var deadIDs []routing.ID

		for r := range ch {
			result.RPC++
			delete(inFlight, r.from.NodeID)

			if r.err != nil {
				result.Timeouts++
				iterLog.Timeouts = append(iterLog.Timeouts, contactLog(r.from))

				if table.RemoveIfDead(ctx, r.from.NodeID) {
					states[r.from.NodeID] = stateDead
					deadIDs = append(deadIDs, r.from.NodeID)
					iterLog.Dead = append(iterLog.Dead, contactLog(r.from))
				} else {
					states[r.from.NodeID] = stateAlive
				}
				continue
			}

			states[r.from.NodeID] = stateAlive
			r.from.MarkVerified()
			table.Add(r.from, nil)

			for _, ct := range r.contacts {
				iterLog.Found = append(iterLog.Found, contactLog(ct))
				if ct.NodeID == target {
					foundTarget = true
				}
				newContacts = append(newContacts, ct)
				if _, ok := states[ct.NodeID]; !ok {
					states[ct.NodeID] = stateUnqueried
				}
			}
		}

		if len(deadIDs) > 0 {
			shortlist = removeDeadFromShortlist(shortlist, states)
		}

		for _, ct := range newContacts {
			if ct.NodeID == local.NodeID {
				continue
			}
			shortlist = append(shortlist, ct)
			table.Add(ct, nil)
		}
		shortlist = uniqueSorted(shortlist, target, cfg.K)

		result.Iterations++
		result.Log.Iterations = append(result.Log.Iterations, iterLog)

		c.ev.Log("lookup_iter", map[string]any{
			"lookup_target": target.Short(),
			"iter":          iter,
			"queried":       queriedShort,
			"rpc":           result.RPC,
			"found_target":  foundTarget,
		})

		if foundTarget {
			break
		}

		allQueried := true
		for _, ct := range shortlist {
			if states[ct.NodeID] == stateUnqueried {
				allQueried = false
				break
			}
		}
		if allQueried {
			break
		}
	}

	filtered := make([]routing.Contact, 0, len(shortlist))
	for _, ct := range shortlist {
		if states[ct.NodeID] != stateDead {
			filtered = append(filtered, ct)
		}
	}
	result.Contacts = filtered

	result.Duration = time.Since(start)
	result.Log.EndUnixMs = time.Now().UnixMilli()
	for _, ct := range result.Contacts {
		result.Log.FinalContacts = append(result.Log.FinalContacts, contactLog(ct))
	}

	foundTarget := false
	for _, ct := range result.Contacts {
		if ct.NodeID == target {
			foundTarget = true
			break
		}
	}

	c.ev.Log("lookup_done", map[string]any{
		"lookup_target": target.Short(),
		"rpc":           result.RPC,
		"iterations":    result.Iterations,
		"timeouts":      result.Timeouts,
		"duration_ms":   result.Duration.Milliseconds(),
		"found_target":  foundTarget,
	})

	return result
}

func removeDeadFromShortlist(shortlist []routing.Contact, states map[routing.ID]candidateState) []routing.Contact {
	out := shortlist[:0]
	for _, ct := range shortlist {
		if states[ct.NodeID] != stateDead {
			out = append(out, ct)
		}
	}
	return out
}

func (c *Client) LookupNodeMultiPath(
	ctx context.Context,
	local routing.Contact,
	seeds []routing.Contact,
	target routing.ID,
	paths int,
	cfg LookupConfig,
	pingFn func(seed routing.Contact) (*routing.RoutingTable, error),
) MultiPathResult {
	start := time.Now()

	if paths <= 0 {
		paths = 1
	}
	if paths > len(seeds) {
		paths = len(seeds)
	}

	result := MultiPathResult{
		Target:    target,
		PathCount: paths,
	}

	c.ev.Log("multipath_start", map[string]any{
		"target": target.Short(),
		"paths":  paths,
	})

	type pathReply struct {
		idx  int
		res  LookupResult
		err  error
		addr string
	}
	ch := make(chan pathReply, paths)
	var wg sync.WaitGroup

	for i := 0; i < paths; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			seed := seeds[idx]

			tbl, err := pingFn(seed)
			if err != nil {
				ch <- pathReply{idx: idx, err: err}
				return
			}

			res := c.LookupNode(ctx, local, tbl, target, cfg)

			addr := ""
			for _, ct := range res.Contacts {
				if ct.NodeID == target {
					addr = ct.Addr()
					break
				}
			}
			ch <- pathReply{idx: idx, res: res, addr: addr}
		}(i)
	}
	wg.Wait()
	close(ch)

	addrVotes := map[string]int{}
	for r := range ch {
		if r.err != nil {
			c.ev.Log("multipath_path_error", map[string]any{
				"idx": r.idx,
				"err": r.err.Error(),
			})
			continue
		}
		result.Paths = append(result.Paths, r.res)
		if r.addr != "" {
			addrVotes[r.addr]++
		}
	}

	bestVotes := 0
	bestAddr := ""
	for addr, votes := range addrVotes {
		if votes > bestVotes {
			bestVotes = votes
			bestAddr = addr
		}
	}

	result.Agreement = bestVotes
	result.Winner = bestAddr
	result.Duration = time.Since(start)

	if bestAddr == "" {
		result.Disagree = true
	} else if bestVotes < (paths+1)/2 {
		result.Disagree = true
	}

	c.ev.Log("multipath_done", map[string]any{
		"target":      target.Short(),
		"paths":       result.PathCount,
		"winner":      bestAddr,
		"agreement":   result.Agreement,
		"disagree":    result.Disagree,
		"duration_ms": result.Duration.Milliseconds(),
		"addr_votes":  addrVotes,
	})

	return result
}

func ExtractTargetAddr(res LookupResult) string {
	for _, ct := range res.Contacts {
		if ct.NodeID == res.Target {
			return ct.Addr()
		}
	}
	return ""
}

func HashAddr(addr string) string {
	h := sha256.Sum256([]byte(addr))
	return fmt.Sprintf("%x", h[:4])
}

func uniqueSorted(nodes []routing.Contact, target routing.ID, n int) []routing.Contact {
	seen := make(map[routing.ID]bool, len(nodes))
	out := nodes[:0]
	for _, ct := range nodes {
		if seen[ct.NodeID] {
			continue
		}
		seen[ct.NodeID] = true
		out = append(out, ct)
	}
	sort.Slice(out, func(i, j int) bool {
		return routing.CloserTo(target, out[i].NodeID, out[j].NodeID)
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

func contactLog(c routing.Contact) ContactLog {
	return ContactLog{NodeID: c.NodeID.String(), Addr: c.Addr()}
}
