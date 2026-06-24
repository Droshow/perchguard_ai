package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

const velocityLowDiversityDefault = 0.3 // fallback when MaxAvgDrift is unconfigured

// SessionAgent governs a single agent session across its full lifetime.
// One SessionAgent exists per unique SessionID; the FleetManager is its owner.
//
// On each tool call it runs three checks in order:
//  1. Intent drift  — how far has this call drifted from the declared user intent?
//  2. Behavior chain — does the call sequence match a known attack pattern?
//  3. Risk gate      — has the cumulative session risk crossed HUMAN_REVIEW / TERMINATE?
type SessionAgent struct {
	sessionID           string
	intent              *IntentModel
	behavior            *BehaviorAnalyzer
	risk                *RiskAccumulator
	st                  store.SessionStore
	cfg                 AgentFleetConfig
	lastVelocityFlagged time.Time // one-fire guard: suppresses re-firing within the same window
}

func newSessionAgent(sessionID string, st store.SessionStore, cfg AgentFleetConfig) *SessionAgent {
	return &SessionAgent{
		sessionID: sessionID,
		intent:    &IntentModel{},
		behavior:  NewBehaviorAnalyzer(cfg.BehaviorWindowSize, cfg.BehaviorChains),
		risk:      &RiskAccumulator{},
		st:        st,
		cfg:       cfg,
	}
}

// Evaluate runs all session-level checks for one tool call admission request.
// Returns a PolicyViolation when risk crosses a threshold; nil when the call is clean.
func (a *SessionAgent) Evaluate(_ context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	state := a.loadOrInit()

	// Establish intent baseline from the first stated user intent.
	if !a.intent.HasBaseline() && req.UserIntent != "" {
		a.intent.SetBaselines([]string{req.UserIntent})
	}

	// Build a text fingerprint of this tool call for drift scoring.
	toolText := req.ToolCall.Name
	for k, v := range req.ToolCall.Parameters {
		toolText += " " + k + " " + fmt.Sprintf("%v", v)
	}

	drift := a.intent.DriftScore(toolText)
	if drift > a.cfg.DriftThreshold {
		a.risk.Add(RiskSignal{
			Contribution: drift * 0.3,
			Reason:       fmt.Sprintf("intent drift %.2f > threshold %.2f", drift, a.cfg.DriftThreshold),
		})
	}

	// Classify the tool call and append to session history (drift stored for velocity analysis).
	stage := ClassifyToolWithMap(req.ToolCall.Name, a.cfg.ToolStageMap)
	event := store.ToolEvent{
		Tool:       req.ToolCall.Name,
		Stage:      string(stage),
		Timestamp:  time.Now(),
		DriftScore: drift,
	}
	state.Events = append(state.Events, event)

	// Attack chain detection runs over the full event window.
	if a.cfg.AttackChainEnabled && a.behavior.DetectChain(state.Events) {
		a.risk.Add(RiskSignal{Contribution: 0.35, Reason: "attack chain sequence detected"})
	}

	// Velocity anomaly: high call frequency with low semantic diversity = probe pattern.
	if contrib := a.velocityContribution(state.Events); contrib > 0 {
		a.risk.Add(RiskSignal{
			Contribution: contrib,
			Reason:       "high-frequency probing with low semantic diversity",
		})
	}

	// Persist updated state.
	state.RiskScore = a.risk.Score()
	if state.RiskScore > state.PeakRiskScore {
		state.PeakRiskScore = state.RiskScore
	}
	state.UpdatedAt = time.Now()
	a.st.Set(a.sessionID, state)

	// Emit a violation when the cumulative risk crosses a threshold.
	score := a.risk.Score()
	switch {
	case score >= RiskThresholdTerminate:
		return &admission.PolicyViolation{
			Layer:    "agent_fleet",
			Policy:   "agentFleet.riskAccumulator",
			Detail:   fmt.Sprintf("session risk %.2f >= terminate threshold %.2f", score, RiskThresholdTerminate),
			Severity: "critical",
			Decision: admission.DecisionTerminate,
		}
	case score >= RiskThresholdHumanReview:
		return &admission.PolicyViolation{
			Layer:    "agent_fleet",
			Policy:   "agentFleet.riskAccumulator",
			Detail:   fmt.Sprintf("session risk %.2f >= review threshold %.2f", score, RiskThresholdHumanReview),
			Severity: "high",
			Decision: admission.DecisionHumanReview,
		}
	default:
		return nil
	}
}

// Risk returns the current cumulative risk score for this session.
func (a *SessionAgent) Risk() float64 { return a.risk.Score() }

// LastDrift returns the most recent per-call intent-drift score for this session.
// Returns 0 when no tool calls have been evaluated yet.
func (a *SessionAgent) LastDrift() float64 {
	s, ok := a.st.Get(a.sessionID)
	if !ok || len(s.Events) == 0 {
		return 0
	}
	return s.Events[len(s.Events)-1].DriftScore
}

// velocityContribution returns a positive risk contribution when the session shows
// high-frequency probing with low semantic diversity in the configured window.
// Returns 0 when the check is disabled or conditions are not met.
// The one-fire guard (lastVelocityFlagged) prevents re-firing within the same window,
// so the contribution is added at most once per window duration.
func (a *SessionAgent) velocityContribution(events []store.ToolEvent) float64 {
	cfg := a.cfg.VelocityAnomaly
	if cfg.ThresholdCalls <= 0 || cfg.WindowMinutes <= 0 || cfg.RiskContribution <= 0 {
		return 0
	}

	windowDur := time.Duration(cfg.WindowMinutes) * time.Minute

	// One-fire guard: skip if we already added velocity risk in this window.
	if !a.lastVelocityFlagged.IsZero() && time.Since(a.lastVelocityFlagged) < windowDur {
		return 0
	}

	cutoff := time.Now().Add(-windowDur)
	var driftSum float64
	count := 0
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Timestamp.Before(cutoff) {
			break
		}
		driftSum += events[i].DriftScore
		count++
	}

	if count < cfg.ThresholdCalls {
		return 0
	}

	maxDrift := cfg.MaxAvgDrift
	if maxDrift <= 0 {
		maxDrift = velocityLowDiversityDefault
	}

	if driftSum/float64(count) >= maxDrift {
		return 0 // diverse calls — not a probe pattern
	}

	a.lastVelocityFlagged = time.Now()
	return cfg.RiskContribution
}

func (a *SessionAgent) loadOrInit() *store.SessionState {
	s, ok := a.st.Get(a.sessionID)
	if !ok {
		s = &store.SessionState{
			SessionID: a.sessionID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		a.st.Set(a.sessionID, s)
	}
	return s
}
