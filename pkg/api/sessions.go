package api

import (
	"net/http"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

type sessionSummary struct {
	SessionID  string    `json:"session_id"`
	RiskScore  float64   `json:"risk_score"`
	EventCount int       `json:"event_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type sessionDetail struct {
	sessionSummary
	Events []eventRecord `json:"events"`
	// IntentBaseline and DriftThreshold support the Art. 26(3) drift-timeline report:
	// the registration-time baseline each event's DriftScore is measured against,
	// and the policy threshold above which drift contributes to risk.
	IntentBaseline string  `json:"intent_baseline,omitempty"`
	DriftThreshold float64 `json:"drift_threshold,omitempty"`
	// IntentPhases and IntentOutOfScope are the manifest-declared Mission.Phases and
	// Mission.OutOfScope vocabulary that seed the additional drift baselines and
	// out-of-scope override (see pkg/agent.IntentModel.DriftScore, §3).
	IntentPhases     []string `json:"intent_phases,omitempty"`
	IntentOutOfScope []string `json:"intent_out_of_scope,omitempty"`
}

type eventRecord struct {
	Tool       string    `json:"tool"`
	Stage      string    `json:"stage"`
	Timestamp  time.Time `json:"timestamp"`
	DriftScore float64   `json:"drift_score"`
}

func (s *APIServer) listSessions(w http.ResponseWriter, r *http.Request) {
	all := s.sessions.List()
	out := make([]sessionSummary, 0, len(all))
	for _, ss := range all {
		out = append(out, sessionSummary{
			SessionID:  ss.SessionID,
			RiskScore:  ss.RiskScore,
			EventCount: len(ss.Events),
			CreatedAt:  ss.CreatedAt,
			UpdatedAt:  ss.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *APIServer) getSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ss, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	events := make([]eventRecord, len(ss.Events))
	for i, e := range ss.Events {
		events[i] = eventRecord{Tool: e.Tool, Stage: e.Stage, Timestamp: e.Timestamp, DriftScore: e.DriftScore}
	}

	var driftThreshold float64
	if lr := s.policyMeta.Load(); lr != nil {
		driftThreshold = lr.Config.Policies.AgentFleet.DriftThreshold
	}

	writeJSON(w, http.StatusOK, sessionDetail{
		sessionSummary: sessionSummary{
			SessionID:  ss.SessionID,
			RiskScore:  ss.RiskScore,
			EventCount: len(ss.Events),
			CreatedAt:  ss.CreatedAt,
			UpdatedAt:  ss.UpdatedAt,
		},
		Events:           events,
		IntentBaseline:   ss.IntentBaseline,
		DriftThreshold:   driftThreshold,
		IntentPhases:     ss.IntentPhases,
		IntentOutOfScope: ss.IntentOutOfScope,
	})
}

func (s *APIServer) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ss, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Emit governance record before eviction so the full session is captured.
	if ss.ManifestID != "" {
		s.EmitGovernanceRecord(ss, false)
	}

	s.sessions.Delete(id)
	s.fleet.Evict(id)
	if s.manifestStore != nil {
		s.manifestStore.Delete(id)
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": id, "status": "terminated"})
}

func (s *APIServer) getSessionBudget(w http.ResponseWriter, r *http.Request) {
	if s.budgetChecker == nil {
		writeError(w, http.StatusServiceUnavailable, "session budget not enabled")
		return
	}
	id := r.PathValue("id")
	snap, ok := s.budgetChecker.Snapshot(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no budget data for session (no tool calls yet, or session unknown)")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// emitGovernanceRecord collects all audit records for the session from the ring buffer
// and writes a governance record via the configured audit.Sink. Always called in the background.
func (s *APIServer) EmitGovernanceRecord(ss *store.SessionState, terminatedEarly bool) {
	records := s.auditRing.Query(store.AuditFilter{SessionID: ss.SessionID, Limit: 1000})

	decisionCounts := make(map[string]int)
	var steps []audit.ToolCallStep
	var events []audit.GovernanceEvent

	// Ring buffer returns newest-first; reverse to emit in chronological order.
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		decisionCounts[rec.Decision]++
		steps = append(steps, audit.ToolCallStep{
			Seq:        len(steps) + 1,
			Timestamp:  rec.Timestamp.UTC().Format(time.RFC3339),
			Tool:       rec.ToolName,
			Decision:   rec.Decision,
			RiskScore:  rec.RiskScore,
			Reason:     rec.Reason,
			PolicyHit:  rec.PolicyHit,
			DataRefsIn: rec.DataRefsIn,
			DataRefOut: rec.DataRefOut,
			DriftScore: rec.DriftScore,
		})
		if rec.Decision != "ALLOW" {
			events = append(events, audit.GovernanceEvent{
				Timestamp: rec.Timestamp.UTC().Format(time.RFC3339),
				Decision:  rec.Decision,
				Tool:      rec.ToolName,
				Reason:    rec.Reason,
				PolicyHit: rec.PolicyHit,
			})
		}
	}

	var driftThreshold float64
	if lr := s.policyMeta.Load(); lr != nil {
		driftThreshold = lr.Config.Policies.AgentFleet.DriftThreshold
	}

	rec := audit.BuildRecord(
		ss.ManifestID,
		ss.ManifestVersion,
		ss.SessionID,
		ss.ParentSessionID,
		s.sessions.Children(ss.SessionID),
		ss.CreatedAt,
		time.Now(),
		steps,
		events,
		decisionCounts,
		ss.PeakRiskScore,
		terminatedEarly,
		ss.IntentBaseline,
		ss.IntentPhases,
		ss.IntentOutOfScope,
		driftThreshold,
	)
	s.auditSink.EmitAsync(rec)
}
