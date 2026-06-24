package api

import (
	"net/http"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

type fleetSummary struct {
	ActiveSessions     int              `json:"active_sessions"`
	HighRiskSessions   int              `json:"high_risk_sessions"`
	RiskDistribution   riskDistrib      `json:"risk_distribution"`
	RecentTerminates   []terminateEvent `json:"recent_terminates"`
	ObservedAt         time.Time        `json:"observed_at"`
	DelegatedSessions  int              `json:"delegated_sessions"`
	MaxDelegationDepth int              `json:"max_delegation_depth"`
}

type riskDistrib struct {
	Low    int `json:"low"`    // risk < 0.4
	Medium int `json:"medium"` // risk 0.4–0.7
	High   int `json:"high"`   // risk > 0.7
}

type terminateEvent struct {
	SessionID string    `json:"session_id"`
	Reason    string    `json:"reason"`
	RiskScore float64   `json:"risk_score"`
	At        time.Time `json:"at"`
}

// getFleetSummary returns the live fleet-level risk picture assembled from the session store
// and the audit ring buffer. No new storage — safe to call at high frequency during a red team run.
func (s *APIServer) getFleetSummary(w http.ResponseWriter, r *http.Request) {
	sessions := s.sessions.List()

	summary := fleetSummary{
		ActiveSessions: len(sessions),
		ObservedAt:     time.Now().UTC(),
	}

	for _, ss := range sessions {
		switch {
		case ss.RiskScore > 0.7:
			summary.RiskDistribution.High++
			summary.HighRiskSessions++
		case ss.RiskScore > 0.4:
			summary.RiskDistribution.Medium++
		default:
			summary.RiskDistribution.Low++
		}
		if ss.ParentSessionID != "" {
			summary.DelegatedSessions++
			if s.delegationStore != nil {
				if d := s.delegationStore.Depth(ss.SessionID); d > summary.MaxDelegationDepth {
					summary.MaxDelegationDepth = d
				}
			}
		}
	}

	terminates := s.auditRing.Query(store.AuditFilter{Decision: "TERMINATE", Limit: 50})
	for _, rec := range terminates {
		summary.RecentTerminates = append(summary.RecentTerminates, terminateEvent{
			SessionID: rec.SessionID,
			Reason:    rec.Reason,
			RiskScore: rec.RiskScore,
			At:        rec.Timestamp,
		})
	}

	writeJSON(w, http.StatusOK, summary)
}
