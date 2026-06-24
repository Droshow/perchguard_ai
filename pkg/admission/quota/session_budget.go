// Package quota implements PerchGuard's resource quota layer.
//
// Analogy to EKS-BankingKube:
//   resource_limits/check_resource_limits.go  → SessionBudgetChecker (token/cost limits)
//   resource_limits/check_resource_requests.go → DepthLimiter        (recursion limits)
//
// Novel concern in agentic systems: "Denial-of-Wallet"
//   An agent loop that calls GPT-4o 10,000 times can cost hundreds of dollars
//   before anyone notices. The quota layer is the first check (fail-fast) because
//   it's purely stateful arithmetic - no LLM needed.
package quota

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// SessionState tracks cumulative resource usage for one agent session.
type SessionState struct {
	SessionID          string
	TokensConsumed     int
	CostUSD            float64
	ToolCallCount      int
	StartedAt          time.Time
	callTimestamps     []time.Time // for per-minute rate limiting
	DelegatedCallLimit int         // >0 overrides policy MaxToolCallsPerSession (set at registration for child sessions)
	mu                 sync.Mutex
}

// BudgetSnapshot is a point-in-time read of one session's budget consumption.
// Served by GET /api/sessions/{id}/budget and consumed by `perchguard meter`.
type BudgetSnapshot struct {
	SessionID       string              `json:"session_id"`
	StartedAt       time.Time           `json:"started_at"`
	ElapsedMinutes  float64             `json:"elapsed_minutes"`
	EstTokens       int                 `json:"estimated_tokens"`
	EstCostUSD      float64             `json:"estimated_cost_usd"`
	ToolCalls       int                 `json:"tool_calls"`
	CallsLastMinute int                 `json:"calls_last_minute"`
	Limits          policy.BudgetLimits `json:"limits"`
}

// SessionBudgetChecker enforces token, cost, and call-rate limits per session.
// This is a stateful checker - it maintains a map of active sessions.
type SessionBudgetChecker struct {
	policy       policy.SessionBudgetPolicy
	sessions     map[string]*SessionState
	mu           sync.RWMutex
	sessionStore store.SessionStore // optional: read DelegatedCallLimit at session init
}

// NewSessionBudgetChecker creates a budget checker.
// sessionStore is optional (nil disables delegation budget enforcement).
func NewSessionBudgetChecker(p policy.SessionBudgetPolicy, sessionStore store.SessionStore) *SessionBudgetChecker {
	return &SessionBudgetChecker{
		policy:       p,
		sessions:     make(map[string]*SessionState),
		sessionStore: sessionStore,
	}
}

func (c *SessionBudgetChecker) Name() string { return "session_budget" }

func (c *SessionBudgetChecker) Check(ctx context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	if !c.policy.Enabled {
		return nil
	}

	state := c.getOrCreateSession(req.SessionID)
	state.mu.Lock()
	defer state.mu.Unlock()

	// Check total token budget
	if state.TokensConsumed >= c.policy.Limits.MaxTokensPerSession {
		return &admission.PolicyViolation{
			Layer:    "quota",
			Policy:   "sessionBudget.maxTokens",
			Detail:   fmt.Sprintf("Session %q has consumed %d tokens (limit: %d)", req.SessionID, state.TokensConsumed, c.policy.Limits.MaxTokensPerSession),
			Severity: "critical",
		}
	}

	// Check cost budget
	if state.CostUSD >= c.policy.Limits.MaxCostPerSessionUSD {
		return &admission.PolicyViolation{
			Layer:    "quota",
			Policy:   "sessionBudget.maxCost",
			Detail:   fmt.Sprintf("Session %q has spent $%.4f (limit: $%.2f)", req.SessionID, state.CostUSD, c.policy.Limits.MaxCostPerSessionUSD),
			Severity: "critical",
		}
	}

	// Check total call count — use delegated limit for child sessions, policy default otherwise.
	callLimit := c.policy.Limits.MaxToolCallsPerSession
	if state.DelegatedCallLimit > 0 {
		callLimit = state.DelegatedCallLimit
	}
	if state.ToolCallCount >= callLimit {
		return &admission.PolicyViolation{
			Layer:    "quota",
			Policy:   "sessionBudget.maxCalls",
			Detail:   fmt.Sprintf("Session %q has made %d tool calls (limit: %d)", req.SessionID, state.ToolCallCount, callLimit),
			Severity: "critical",
		}
	}

	// Check per-minute rate limit
	if c.policy.Limits.MaxToolCallsPerMinute > 0 {
		oneMinuteAgo := time.Now().Add(-time.Minute)
		recentCount := 0
		for _, ts := range state.callTimestamps {
			if ts.After(oneMinuteAgo) {
				recentCount++
			}
		}
		if recentCount >= c.policy.Limits.MaxToolCallsPerMinute {
			return &admission.PolicyViolation{
				Layer:    "quota",
				Policy:   "sessionBudget.rateLimit",
				Detail:   fmt.Sprintf("Session %q exceeded rate limit: %d calls/min (limit: %d)", req.SessionID, recentCount, c.policy.Limits.MaxToolCallsPerMinute),
				Severity: "high",
			}
		}
	}

	return nil
}

// Record updates the session state after a tool call is allowed.
// Called by the Interceptor after the full pipeline completes.
func (c *SessionBudgetChecker) Record(ctx context.Context, req *admission.ToolCallAdmissionRequest) {
	if !c.policy.Enabled {
		return
	}

	state := c.getOrCreateSession(req.SessionID)
	state.mu.Lock()
	defer state.mu.Unlock()

	state.ToolCallCount++
	state.callTimestamps = append(state.callTimestamps, time.Now())

	// Token count from metadata (set by mcp proxy estimator or agent framework).
	if tokensStr, ok := req.Metadata["tokens_used"]; ok {
		if tokens, err := strconv.Atoi(tokensStr); err == nil {
			state.TokensConsumed += tokens
			state.CostUSD += TokenCostUSD(tokens, req.Metadata["model"])
		}
	}

	// Prune old timestamps to keep memory bounded
	oneMinuteAgo := time.Now().Add(-time.Minute)
	pruned := state.callTimestamps[:0]
	for _, ts := range state.callTimestamps {
		if ts.After(oneMinuteAgo) {
			pruned = append(pruned, ts)
		}
	}
	state.callTimestamps = pruned
}

func (c *SessionBudgetChecker) getOrCreateSession(sessionID string) *SessionState {
	c.mu.Lock()
	defer c.mu.Unlock()

	if s, ok := c.sessions[sessionID]; ok {
		return s
	}
	s := &SessionState{
		SessionID: sessionID,
		StartedAt: time.Now(),
	}
	// Read delegated call limit from the session store for child sessions.
	if c.sessionStore != nil {
		if ss, ok := c.sessionStore.Get(sessionID); ok && ss.DelegatedCallLimit > 0 {
			s.DelegatedCallLimit = ss.DelegatedCallLimit
		}
	}
	c.sessions[sessionID] = s
	return s
}

// Snapshot returns a point-in-time budget reading for the named session.
// Returns false if the session has not made any tool calls yet.
func (c *SessionBudgetChecker) Snapshot(sessionID string) (*BudgetSnapshot, bool) {
	c.mu.RLock()
	s, ok := c.sessions[sessionID]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	oneMinAgo := time.Now().Add(-time.Minute)
	callsLastMin := 0
	for _, ts := range s.callTimestamps {
		if ts.After(oneMinAgo) {
			callsLastMin++
		}
	}
	return &BudgetSnapshot{
		SessionID:       s.SessionID,
		StartedAt:       s.StartedAt,
		ElapsedMinutes:  time.Since(s.StartedAt).Minutes(),
		EstTokens:       s.TokensConsumed,
		EstCostUSD:      s.CostUSD,
		ToolCalls:       s.ToolCallCount,
		CallsLastMinute: callsLastMin,
		Limits:          c.policy.Limits,
	}, true
}

// ReloadPolicy swaps the policy limits without losing session state.
// Called by the policy watcher on hot-reload.
func (c *SessionBudgetChecker) ReloadPolicy(p policy.SessionBudgetPolicy) {
	c.mu.Lock()
	c.policy = p
	c.mu.Unlock()
}

// DepthLimiter prevents agent-calling-agent recursion spirals.
// Analogous to how K8s prevents runaway init container chains.
type DepthLimiter struct {
	policy policy.DepthLimiterPolicy
}

func NewDepthLimiter(p policy.DepthLimiterPolicy) *DepthLimiter {
	return &DepthLimiter{policy: p}
}

func (d *DepthLimiter) Name() string { return "depth_limiter" }

func (d *DepthLimiter) Check(ctx context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	if !d.policy.Enabled {
		return nil
	}

	if req.NestingDepth >= d.policy.MaxAgentNestingDepth {
		return &admission.PolicyViolation{
			Layer:    "quota",
			Policy:   "depthLimiter.maxNesting",
			Detail:   fmt.Sprintf("Agent nesting depth %d exceeds maximum %d - potential recursion loop", req.NestingDepth, d.policy.MaxAgentNestingDepth),
			Severity: "critical",
		}
	}

	return nil
}

func (d *DepthLimiter) Record(ctx context.Context, req *admission.ToolCallAdmissionRequest) {
	// Depth is tracked by the agent framework, not by PerchGuard
}
