package rpc

import (
	"sort"
	"sync"
	"time"

	"p2pnode/internal/routing"
)

type LookupConfig struct {
	Alpha   int
	K       int
	Timeout time.Duration
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
}

type ContactLog struct {
	NodeID string `json:"node_id"`
	Addr   string `json:"addr"`
}

func (c *Client) LookupNode(
	local routing.Contact,
	table *routing.RoutingTable,
	target routing.ID,
	cfg LookupConfig,
) LookupResult {
	start := time.Now()
	result := LookupResult{
		Target: target,
		Log: LookupLog{
			Target:      target.String(),
			Initiator:   local.NodeID.String(),
			StartUnixMs: start.UnixMilli(),
		},
	}

	shortlist := table.Closest(target, cfg.K)
	queried := make(map[routing.ID]bool)

	for iter := 0; iter < routing.IDBits; iter++ {
		var toQuery []routing.Contact
		for _, ct := range shortlist {
			if queried[ct.NodeID] {
				continue
			}
			if ct.NodeID == local.NodeID {
				continue
			}
			toQuery = append(toQuery, ct)
			queried[ct.NodeID] = true
			if len(toQuery) >= cfg.Alpha {
				break
			}
		}
		if len(toQuery) == 0 {
			break
		}

		iterLog := LookupIteration{Iter: iter}
		for _, ct := range toQuery {
			iterLog.Queried = append(iterLog.Queried, contactLog(ct))
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
				contacts, err := c.FindNodeRPC(local, ct, target, cfg.Timeout)
				ch <- reply{from: ct, contacts: contacts, err: err}
			}(ct)
		}
		wg.Wait()
		close(ch)

		var newContacts []routing.Contact
		foundTarget := false
		for r := range ch {
			result.RPC++
			if r.err != nil {
				result.Timeouts++
				iterLog.Timeouts = append(iterLog.Timeouts, contactLog(r.from))
				table.Remove(r.from.NodeID)
				continue
			}
			r.from.MarkVerified()
			table.Add(r.from, nil)
			for _, ct := range r.contacts {
				iterLog.Found = append(iterLog.Found, contactLog(ct))
				if ct.NodeID == target {
					foundTarget = true
				}
				newContacts = append(newContacts, ct)
			}
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

		if foundTarget {
			break
		}

		allQueried := true
		for _, ct := range shortlist {
			if !queried[ct.NodeID] {
				allQueried = false
				break
			}
		}
		if allQueried {
			break
		}
	}

	result.Contacts = shortlist
	result.Duration = time.Since(start)
	result.Log.EndUnixMs = time.Now().UnixMilli()
	for _, ct := range shortlist {
		result.Log.FinalContacts = append(result.Log.FinalContacts, contactLog(ct))
	}
	return result
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
