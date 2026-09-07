// Package agent implements the PerchGuard Phase 3 session-aware agent fleet.
//
// The fleet adds a stateful reasoning layer on top of the existing per-call
// admission pipeline. Where Phase 1/2 validators inspect individual tool calls
// in isolation, the fleet tracks the full arc of an agent session and detects
// threats that only become visible across multiple calls — intent drift,
// privilege escalation chains, and runaway risk accumulation.
//
// Integration: FleetManager implements admission.Validator so it slots into the
// existing Interceptor pipeline with zero changes to the admission engine.
package agent

import (
	"context"
	"sync"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/pii"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// AgentFleetConfig holds the tuneable parameters loaded from policies.yaml.
type AgentFleetConfig struct {
	// DriftThreshold is the cosine drift score above which a tool call is flagged.
	// Range 0.0–1.0; lower = stricter. Recommended: 0.4.
	DriftThreshold float64

	// BehaviorWindowSize is the number of recent tool events the attack chain
	// detector considers. Default: 10.
	BehaviorWindowSize int

	// AttackChainEnabled toggles the behavior sequence detector.
	AttackChainEnabled bool

	// BehaviorChains is the set of stage sequences to detect.
	// When empty, the built-in default chains are used as a fallback.
	BehaviorChains [][]Stage

	// ToolStageMap is the policy-driven tool→stage mapping.
	// When nil, ClassifyTool falls back to the built-in hardcoded switch.
	ToolStageMap map[string][]string

	// VelocityAnomaly configures the high-frequency low-diversity probe detector.
	VelocityAnomaly VelocityAnomalyConfig

	// IntentRedactor scrubs PII/biometric content from intentText before it is
	// persisted as SessionState.IntentBaseline (see EUAIACT-PERCHGUARD-SYNERGY.md §5).
	// Nil-safe: a nil or pattern-less matcher leaves intentText unchanged.
	IntentRedactor *pii.Matcher
}

// VelocityAnomalyConfig parameters for the velocity-based behavioral anomaly signal.
type VelocityAnomalyConfig struct {
	WindowMinutes    int
	ThresholdCalls   int
	MaxAvgDrift      float64
	RiskContribution float64
}

// FleetManager is the admission.Validator that gates every tool call through
// its per-session SessionAgent. It owns the session lifecycle: create on first
// call, evict on TERMINATE (not yet implemented — placeholder for Phase 4).
type FleetManager struct {
	mu     sync.RWMutex
	agents map[string]*SessionAgent
	store  store.SessionStore
	cfg    AgentFleetConfig
}

// NewFleetManager creates a FleetManager backed by the given SessionStore.
// Use store.NewMemoryStore() for local development.
func NewFleetManager(cfg AgentFleetConfig, st store.SessionStore) *FleetManager {
	return &FleetManager{
		agents: make(map[string]*SessionAgent),
		store:  st,
		cfg:    cfg,
	}
}

// Name satisfies the admission.Validator interface.
func (f *FleetManager) Name() string { return "agent_fleet" }

// Validate runs the session agent for the request's SessionID.
// A new SessionAgent is created the first time a session is seen.
// Returns nil when the call is clean; a PolicyViolation when risk is elevated.
func (f *FleetManager) Validate(ctx context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	agent := f.getOrCreate(req.SessionID)
	return agent.Evaluate(ctx, req)
}

// Evict removes a session from the in-memory agent map.
// Called by DELETE /api/sessions/{id} to make termination immediate.
// The SessionStore entry is deleted separately by the API handler.
func (f *FleetManager) Evict(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.agents, sessionID)
}

// SeedFromManifest pre-creates a session with its intent baseline set from the agent
// manifest before the first tool call arrives. Called by POST /agents/register.
// phases are the manifest's Mission.Phases — each seeds an additional drift baseline
// so a call on-task for a later phase doesn't false-positive against the registration-time
// baseline alone (see EUAIACT-PERCHGUARD-SYNERGY.md §3). outOfScope is Mission.OutOfScope —
// a call resembling this vocabulary scores as drift regardless of how close it is to a
// declared phase (see IntentModel.DriftScore).
// parentSessionID is non-empty for delegated sub-agent sessions.
func (f *FleetManager) SeedFromManifest(sessionID, intentText string, phases, outOfScope []string, manifestID, manifestVersion, parentSessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	a := newSessionAgent(sessionID, f.store, f.cfg)
	a.intent.SetBaselines(append([]string{intentText}, phases...))
	a.intent.SetOutOfScope(outOfScope)
	f.agents[sessionID] = a

	now := time.Now()
	// IntentBaseline, IntentPhases, and IntentOutOfScope persist the declared-intent text that
	// seeded the drift baselines above (see EUAIACT-PERCHGUARD-SYNERGY.md §2-3, §5). intentText
	// can include up to ~800 chars of PRD/snapshot content, so all three are redacted through
	// IntentRedactor before persistence — the drift baselines above keep the raw vocabulary;
	// only the exported/persisted copies are scrubbed.
	intentBaseline := intentText
	intentPhases := phases
	intentOutOfScope := outOfScope
	if f.cfg.IntentRedactor != nil {
		intentBaseline = f.cfg.IntentRedactor.Redact(intentBaseline)
		intentPhases = redactAll(f.cfg.IntentRedactor, phases)
		intentOutOfScope = redactAll(f.cfg.IntentRedactor, outOfScope)
	}
	f.store.Set(sessionID, &store.SessionState{
		SessionID:        sessionID,
		ManifestID:       manifestID,
		ManifestVersion:  manifestVersion,
		IntentBaseline:   intentBaseline,
		IntentPhases:     intentPhases,
		IntentOutOfScope: intentOutOfScope,
		ParentSessionID:  parentSessionID,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
}

// redactAll applies m.Redact to each element of texts, returning a new slice.
func redactAll(m *pii.Matcher, texts []string) []string {
	if len(texts) == 0 {
		return texts
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = m.Redact(t)
	}
	return out
}

// SessionRisk returns the current cumulative risk score for sessionID.
// Returns 0 when the session is not yet tracked. Implements admission.RiskScorer.
func (f *FleetManager) SessionRisk(sessionID string) float64 {
	f.mu.RLock()
	a, ok := f.agents[sessionID]
	f.mu.RUnlock()
	if !ok {
		return 0
	}
	return a.Risk()
}

// SessionDrift returns this call's intent-drift score for sessionID.
// Returns 0 when the session is not yet tracked or has no scored calls.
// Implements admission.DriftScorer.
func (f *FleetManager) SessionDrift(sessionID string) float64 {
	f.mu.RLock()
	a, ok := f.agents[sessionID]
	f.mu.RUnlock()
	if !ok {
		return 0
	}
	return a.LastDrift()
}

// getOrCreate returns the existing SessionAgent for sessionID, creating one if needed.
func (f *FleetManager) getOrCreate(sessionID string) *SessionAgent {
	f.mu.RLock()
	a, ok := f.agents[sessionID]
	f.mu.RUnlock()
	if ok {
		return a
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// Double-check after acquiring write lock.
	if a, ok = f.agents[sessionID]; ok {
		return a
	}
	a = newSessionAgent(sessionID, f.store, f.cfg)
	f.agents[sessionID] = a
	return a
}
