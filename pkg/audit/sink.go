// Package audit defines the governance record types and sink interface.
// All types that describe what an agent did live here — they are PerchGuard-native
// and do not depend on any external storage system.
package audit

import (
	"fmt"
	"time"
)

// Sink writes governance records for completed agent sessions.
// Implementations may write to a local file, Postgres, a webhook, or any project store.
type Sink interface {
	EmitAsync(rec GovernanceRecord)
}

// GovernanceRecord is the PerchGuard audit entry for one completed agent session.
type GovernanceRecord struct {
	ID             string         `json:"id"`
	Timestamp      string         `json:"timestamp"`
	Summary        string         `json:"summary"`
	Source         string         `json:"source"` // always "perchguard"
	SessionContext SessionSummary `json:"session_context"`
}

// SessionSummary carries the full governance record for one agent session.
type SessionSummary struct {
	AgentID          string            `json:"agent_id"`
	ManifestVersion  string            `json:"manifest_version"`
	SessionID        string            `json:"session_id"`
	ParentSessionID  string            `json:"parent_session_id,omitempty"`
	ChildSessions    []string          `json:"child_sessions,omitempty"`
	Started          string            `json:"started"`
	Ended            string            `json:"ended"`
	ToolCalls        int               `json:"tool_calls"`
	Decisions        map[string]int    `json:"decisions"`
	PeakRiskScore    float64           `json:"peak_risk_score"`
	TerminatedEarly  bool              `json:"terminated_early"`
	Steps            []ToolCallStep    `json:"steps"`
	GovernanceEvents []GovernanceEvent `json:"governance_events,omitempty"`
	// IntentBaseline is the fused manifest+context text the session's intent baseline
	// was set from at registration (write-once). See EUAIACT-PERCHGUARD-SYNERGY.md §2-3.
	IntentBaseline string `json:"intent_baseline,omitempty"`
	// IntentPhases are the manifest's declared Mission.Phases — each seeded an
	// additional drift baseline at registration (write-once). See §3.
	IntentPhases []string `json:"intent_phases,omitempty"`
	// IntentOutOfScope is the manifest's declared Mission.OutOfScope vocabulary —
	// a call resembling it scores as drift regardless of baseline proximity (write-once). See §3.
	IntentOutOfScope []string `json:"intent_out_of_scope,omitempty"`
	// DriftThreshold is the cosine drift score above which Steps[].DriftScore was
	// flagged as a risk contribution, per policy at session end.
	DriftThreshold float64 `json:"drift_threshold,omitempty"`
}

// ToolCallStep is one intercept decision in the ordered step-by-step agent trace.
// Every tool call — ALLOW or otherwise — appears here in chronological order.
type ToolCallStep struct {
	Seq        int      `json:"seq"`
	Timestamp  string   `json:"timestamp"`
	Tool       string   `json:"tool"`
	Decision   string   `json:"decision"`
	RiskScore  float64  `json:"risk_score"`
	Reason     string   `json:"reason,omitempty"`
	PolicyHit  string   `json:"policy_hit,omitempty"`
	DataRefsIn []string `json:"data_refs_in,omitempty"`
	DataRefOut string   `json:"data_ref_out,omitempty"`
	DriftScore *float64 `json:"drift_score,omitempty"` // intent drift for this call; nil when no fleet validator scored it
}

// GovernanceEvent is one notable (non-ALLOW) admission decision within a session.
type GovernanceEvent struct {
	Timestamp string `json:"timestamp"`
	Decision  string `json:"decision"`
	Tool      string `json:"tool"`
	Reason    string `json:"reason"`
	PolicyHit string `json:"policy_hit,omitempty"`
}

// NoOpSink discards all records. Used when no sink is configured.
type NoOpSink struct{}

func (NoOpSink) EmitAsync(GovernanceRecord) {}

// BuildRecord constructs a GovernanceRecord from raw session data.
func BuildRecord(
	agentID, manifestVersion, sessionID, parentSessionID string,
	childSessions []string,
	started, ended time.Time,
	steps []ToolCallStep,
	events []GovernanceEvent,
	decisions map[string]int,
	peakRisk float64,
	terminatedEarly bool,
	intentBaseline string,
	intentPhases []string,
	intentOutOfScope []string,
	driftThreshold float64,
) GovernanceRecord {
	total := 0
	for _, c := range decisions {
		total += c
	}
	return GovernanceRecord{
		ID:        fmt.Sprintf("snap-%s", ended.Format("20060102-150405")),
		Timestamp: ended.UTC().Format(time.RFC3339),
		Summary: fmt.Sprintf(
			"Governance session: %s v%s — %d calls, peak risk %.2f",
			agentID, manifestVersion, total, peakRisk,
		),
		Source: "perchguard",
		SessionContext: SessionSummary{
			AgentID:          agentID,
			ManifestVersion:  manifestVersion,
			SessionID:        sessionID,
			ParentSessionID:  parentSessionID,
			ChildSessions:    childSessions,
			Started:          started.UTC().Format(time.RFC3339),
			Ended:            ended.UTC().Format(time.RFC3339),
			ToolCalls:        total,
			Decisions:        decisions,
			PeakRiskScore:    peakRisk,
			TerminatedEarly:  terminatedEarly,
			Steps:            steps,
			GovernanceEvents: events,
			IntentBaseline:   intentBaseline,
			IntentPhases:     intentPhases,
			IntentOutOfScope: intentOutOfScope,
			DriftThreshold:   driftThreshold,
		},
	}
}
