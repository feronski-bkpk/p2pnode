package metrics

type NetworkReport struct {
	GeneratedAt int64           `json:"generated_at_ms"`
	N           int             `json:"n"`
	K           int             `json:"k"`
	Nodes       []NodeSummary   `json:"nodes"`
	Criteria    NetworkCriteria `json:"criteria"`
}

type NodeSummary struct {
	NodeID        string `json:"node_id"`
	Addr          string `json:"addr"`
	TableSize     int    `json:"table_size"`
	BucketCount   int    `json:"bucket_count"`
	MaxBucketFill int    `json:"max_bucket_fill"`
	MinBucketFill int    `json:"min_bucket_fill"`
}

type NetworkCriteria struct {
	PctWithLessThanNMinus1     float64 `json:"pct_with_less_than_n_minus_1"`
	Passes80PctLessThanNMinus1 bool    `json:"passes_80pct_less_than_n_minus_1"`
	NoFullRegistry             bool    `json:"no_full_registry"`
	TotalContactsLtNSquared    bool    `json:"total_contacts_lt_n_squared"`
	Degenerate                 bool    `json:"degenerate"`
}

func BuildNetworkReport(k int, snapshots []RoutingSnapshot) NetworkReport {
	n := len(snapshots)
	rep := NetworkReport{
		GeneratedAt: nowMs(),
		N:           n,
		K:           k,
	}

	fullRegistryCount := 0
	lessThanNMinus1 := 0
	totalContacts := 0

	for _, s := range snapshots {
		rep.Nodes = append(rep.Nodes, NodeSummary{
			NodeID:        s.NodeID,
			Addr:          s.FirstAddr(),
			TableSize:     s.Size,
			BucketCount:   s.BucketCount,
			MaxBucketFill: s.MaxFill,
			MinBucketFill: s.MinFill,
		})
		if s.Size >= n-1 {
			fullRegistryCount++
		}
		if s.Size < n-1 {
			lessThanNMinus1++
		}
		totalContacts += s.Size
	}

	if n > 0 {
		rep.Criteria.PctWithLessThanNMinus1 = float64(lessThanNMinus1) / float64(n) * 100.0
	}
	rep.Criteria.Passes80PctLessThanNMinus1 = rep.Criteria.PctWithLessThanNMinus1 >= 80.0
	rep.Criteria.NoFullRegistry = fullRegistryCount == 0
	rep.Criteria.TotalContactsLtNSquared = totalContacts < n*(n-1)
	rep.Criteria.Degenerate = !rep.Criteria.Passes80PctLessThanNMinus1 ||
		!rep.Criteria.NoFullRegistry

	return rep
}

func (s RoutingSnapshot) FirstAddr() string {
	for _, b := range s.Buckets {
		if len(b.Contacts) > 0 {
			return b.Contacts[0].Addr
		}
	}
	return ""
}

func (e *Exporter) ExportNetworkReport(rep NetworkReport, name string) (string, error) {
	return e.WriteJSON(name, rep)
}
