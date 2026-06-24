package api

import (
	"net/http"
	"sort"

	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// Stats is the aggregate view of PerchGuard decisions.
type Stats struct {
	TotalSessions  int            `json:"total_sessions"`
	Decisions      map[string]int `json:"decisions"`
	TopDeniedTools []toolCount    `json:"top_denied_tools"`
	PolicyVersion  string         `json:"policy_version"` // sha256[:8] of the active policies.yaml
}

type toolCount struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

func (s *APIServer) getStats(w http.ResponseWriter, r *http.Request) {
	sessions := s.sessions.List()

	// Pull up to 1000 most recent records from the ring buffer.
	records := s.auditRing.Query(store.AuditFilter{Limit: 1000})
	decisions := make(map[string]int)
	deniedTools := make(map[string]int)
	for _, rec := range records {
		decisions[rec.Decision]++
		if rec.Decision == "DENY" || rec.Decision == "TERMINATE" {
			deniedTools[rec.ToolName]++
		}
	}

	var policyVersion string
	if lr := s.policyMeta.Load(); lr != nil {
		policyVersion = lr.Hash
	}
	writeJSON(w, http.StatusOK, Stats{
		TotalSessions:  len(sessions),
		Decisions:      decisions,
		TopDeniedTools: topN(deniedTools, 5),
		PolicyVersion:  policyVersion,
	})
}

// topN returns the n most frequent tool names from a count map, sorted descending.
func topN(counts map[string]int, n int) []toolCount {
	out := make([]toolCount, 0, len(counts))
	for tool, count := range counts {
		out = append(out, toolCount{Tool: tool, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
