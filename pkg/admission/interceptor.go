// Package admission is the core of PerchGuard's agentic admission control.
//
// Pipeline:  Request → [Quota Check] → [Validation] → [Mutation] → Decision
//            Output  → [Outbound Validation] → Decision (with sanitized output)
package admission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Validator is the interface every inbound validation check must implement.
type Validator interface {
	Name() string
	Validate(ctx context.Context, req *ToolCallAdmissionRequest) *PolicyViolation
}

// Mutator transforms tool call parameters to be safer.
type Mutator interface {
	Name() string
	Mutate(ctx context.Context, req *ToolCallAdmissionRequest) (*ToolCall, error)
}

// QuotaChecker tracks session-level resource usage.
type QuotaChecker interface {
	Name() string
	Check(ctx context.Context, req *ToolCallAdmissionRequest) *PolicyViolation
	Record(ctx context.Context, req *ToolCallAdmissionRequest)
}

// AuditLogger writes admission decisions to the audit trail.
type AuditLogger interface {
	Log(entry AuditEntry)
}

// TokenVerifier validates session tokens issued at manifest registration.
// When a request carries X-PerchGuard-Agent-Token, the interceptor calls Verify.
// A nil TokenVerifier means token verification is disabled (backward-compatible default).
type TokenVerifier interface {
	Verify(sessionID, agentID, token string) bool
}

// ManifestLookup resolves a session ID to the agent's declared mission context.
// Called by the interceptor to enrich each request before validators run.
// Return nil when the session has no registered manifest (unregistered agents).
type ManifestLookup func(sessionID string) *MissionContext

// Interceptor is the PerchGuard admission engine.
type Interceptor struct {
	mu               sync.RWMutex
	validators       []Validator
	outboundVals     []OutboundValidator
	mutators         []Mutator
	quotas           []QuotaChecker
	auditLog         AuditLogger
	reviewDispatcher ReviewDispatcher // nil = no human review webhook
	tokenVerifier    TokenVerifier    // nil = token verification disabled
	manifestLookup   ManifestLookup   // nil = mission context not enriched
	observeMode      bool             // when true, pipeline runs but all decisions are overridden to ALLOW
}

// InterceptorOption configures the Interceptor.
type InterceptorOption func(*Interceptor)

func WithOutboundValidators(vs ...OutboundValidator) InterceptorOption {
	return func(i *Interceptor) { i.outboundVals = vs }
}

func WithReviewDispatcher(d ReviewDispatcher) InterceptorOption {
	return func(i *Interceptor) { i.reviewDispatcher = d }
}

func WithTokenVerifier(v TokenVerifier) InterceptorOption {
	return func(i *Interceptor) { i.tokenVerifier = v }
}

func WithManifestLookup(fn ManifestLookup) InterceptorOption {
	return func(i *Interceptor) { i.manifestLookup = fn }
}

func WithObserveMode(observe bool) InterceptorOption {
	return func(i *Interceptor) { i.observeMode = observe }
}

// ObserveMode reports whether the interceptor is running in audit-only mode.
func (i *Interceptor) ObserveMode() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.observeMode
}

// SetObserveMode updates the enforcement mode at runtime (e.g. on policy hot-reload).
func (i *Interceptor) SetObserveMode(observe bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.observeMode = observe
}

// NewInterceptor constructs the admission pipeline.
// Variadic opts are backward-compatible additions (existing callers unchanged).
func NewInterceptor(validators []Validator, mutators []Mutator, quotas []QuotaChecker, audit AuditLogger, opts ...InterceptorOption) *Interceptor {
	i := &Interceptor{
		validators: validators,
		mutators:   mutators,
		quotas:     quotas,
		auditLog:   audit,
	}
	for _, o := range opts {
		o(i)
	}
	return i
}

// ReloadValidators atomically swaps the policy-driven pipeline slices.
// Called by the policy watcher when configs/policies.yaml changes on disk.
// The lock is held only for the slice swap — validation work runs outside the lock.
func (i *Interceptor) ReloadValidators(validators []Validator, mutators []Mutator, quotas []QuotaChecker) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.validators = validators
	i.mutators = mutators
	i.quotas = quotas
}

// ValidatorNames returns the names of all active inbound validators.
func (i *Interceptor) ValidatorNames() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	names := make([]string, len(i.validators))
	for j, v := range i.validators {
		names[j] = v.Name()
	}
	return names
}

// MutatorNames returns the names of all active mutators.
func (i *Interceptor) MutatorNames() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	names := make([]string, len(i.mutators))
	for j, m := range i.mutators {
		names[j] = m.Name()
	}
	return names
}

// QuotaNames returns the names of all active quota checkers.
func (i *Interceptor) QuotaNames() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	names := make([]string, len(i.quotas))
	for j, q := range i.quotas {
		names[j] = q.Name()
	}
	return names
}

// lineageRef computes the deterministic data-provenance identifier for one call's output.
// Matches store.LineageRef — kept local to avoid importing pkg/store into pkg/admission.
func lineageRef(sessionID, uid string) string {
	h := sha256.Sum256([]byte(sessionID + ":" + uid))
	return hex.EncodeToString(h[:6])
}

// Intercept runs one ToolCallAdmissionRequest through the inbound pipeline.
// Pipeline order: Quota → Validation → Mutation → Decision.
func (i *Interceptor) Intercept(ctx context.Context, req *ToolCallAdmissionRequest) *ToolCallAdmissionResponse {
	ctx, span := otel.Tracer("perchguard").Start(ctx, "intercept")
	defer span.End()
	span.SetAttributes(
		attribute.String("session_id", req.SessionID),
		attribute.String("agent_id", req.AgentID),
		attribute.String("agent_role", req.AgentRole),
		attribute.String("tool", req.ToolCall.Name),
	)

	// Snapshot slice references under read lock so ReloadValidators can swap
	// concurrently without blocking ongoing requests.
	i.mu.RLock()
	quotas := i.quotas
	validators := i.validators
	mutators := i.mutators
	i.mu.RUnlock()

	start := time.Now()
	req.Timestamp = start

	// Lineage ref — deterministic identifier for this call's output, returned in all responses.
	dataRefOut := lineageRef(req.SessionID, req.UID)

	// Phase 1: Quota (cheap, fail-fast)
	for _, q := range quotas {
		_, qSpan := otel.Tracer("perchguard").Start(ctx, "quota."+q.Name())
		v := q.Check(ctx, req)
		if v != nil {
			qSpan.SetStatus(codes.Error, v.Detail)
			qSpan.End()
			resp := i.denyResponse(req, v, DecisionTerminate)
			resp.DataRefOut = dataRefOut
			span.SetAttributes(attribute.String("decision", string(DecisionTerminate)))
			span.SetStatus(codes.Error, resp.Reason)
			i.audit(req, resp, start)
			return resp
		}
		qSpan.End()
	}

	// Phase 2: Validation (collect all violations; each validator gets its own child span)
	var violations []PolicyViolation
	for _, v := range validators {
		_, vSpan := otel.Tracer("perchguard").Start(ctx, "validator."+v.Name())
		viol := v.Validate(ctx, req)
		if viol != nil {
			vSpan.SetStatus(codes.Error, viol.Detail)
			violations = append(violations, *viol)
		}
		vSpan.End()
	}

	if len(violations) > 0 {
		decision := resolveDecision(violations)
		policyHit := ""
		for _, viol := range violations {
			if viol.Decision == decision {
				policyHit = viol.Policy
				break
			}
		}
		resp := &ToolCallAdmissionResponse{
			UID:           req.UID,
			Decision:      decision,
			Reason:        summarizeViolations(violations),
			Violations:    violations,
			PolicyMatched: policyHit,
			DataRefOut:    dataRefOut,
		}
		span.SetAttributes(attribute.String("decision", string(decision)))
		span.SetStatus(codes.Error, resp.Reason)
		if decision == DecisionHumanReview {
			resp.HumanReviewContext = buildReviewContext(req, violations)
			if i.reviewDispatcher != nil {
				timeout := time.Duration(i.reviewDispatcher.TimeoutSeconds()) * time.Second
				reviewCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				approved, err := i.reviewDispatcher.Dispatch(reviewCtx, ReviewRequest{
					RequestUID: req.UID,
					SessionID:  req.SessionID,
					AgentID:    req.AgentID,
					ToolCall:   req.ToolCall,
					UserIntent: req.UserIntent,
					Violations: violations,
					Summary:    resp.HumanReviewContext.Summary,
				})
				if err == nil && approved {
					resp.Decision = DecisionAllow
					resp.Reason = "human review approved"
				} else {
					resp.Decision = DecisionDeny
					if err != nil {
						resp.Reason = "human review failed: " + err.Error()
					} else {
						resp.Reason = "human review denied"
					}
				}
			}
		}
		attachRisk(validators, req.SessionID, resp)
		attachDrift(validators, req.SessionID, resp)
		i.audit(req, resp, start)
		return resp
	}

	// Phase 3: Mutation
	mutatedCall := &req.ToolCall
	for _, m := range mutators {
		_, mSpan := otel.Tracer("perchguard").Start(ctx, "mutator."+m.Name())
		result, err := m.Mutate(ctx, req)
		mSpan.End()
		if err != nil {
			log.Printf("[perchguard] mutator %s error: %v", m.Name(), err)
			continue
		}
		if result != nil {
			mutatedCall = result
		}
	}

	decision := DecisionAllow
	var mutatedResult *ToolCall
	if !reflect.DeepEqual(*mutatedCall, req.ToolCall) {
		decision = DecisionMutate
		mutatedResult = mutatedCall
	}

	resp := &ToolCallAdmissionResponse{
		UID:         req.UID,
		Decision:    decision,
		MutatedCall: mutatedResult,
		Reason:      "all checks passed",
		DataRefOut:  dataRefOut,
	}
	for _, q := range quotas {
		q.Record(ctx, req)
	}
	attachRisk(validators, req.SessionID, resp)
	attachDrift(validators, req.SessionID, resp)
	span.SetAttributes(attribute.String("decision", string(resp.Decision)))
	i.audit(req, resp, start)
	return resp
}

// InterceptOutput runs the outbound pipeline on a tool result before it is
// fed back to the agent. Called by ServeHTTP for POST /validate/output.
func (i *Interceptor) InterceptOutput(ctx context.Context, req *ToolCallAdmissionRequest) *ToolCallAdmissionResponse {
	ctx, span := otel.Tracer("perchguard").Start(ctx, "intercept_output")
	defer span.End()
	span.SetAttributes(
		attribute.String("session_id", req.SessionID),
	)

	i.mu.RLock()
	outboundVals := i.outboundVals
	i.mu.RUnlock()

	start := time.Now()

	var violations []PolicyViolation
	for _, v := range outboundVals {
		_, vSpan := otel.Tracer("perchguard").Start(ctx, "outbound."+v.Name())
		viol := v.Validate(ctx, req)
		if viol != nil {
			vSpan.SetStatus(codes.Error, viol.Detail)
			violations = append(violations, *viol)
		}
		vSpan.End()
	}

	if len(violations) == 0 {
		resp := &ToolCallAdmissionResponse{
			UID:      req.UID,
			Decision: DecisionAllow,
			Reason:   "output validation passed",
		}
		i.audit(req, resp, start)
		return resp
	}

	decision := resolveOutboundDecision(violations)
	resp := &ToolCallAdmissionResponse{
		UID:        req.UID,
		Decision:   decision,
		Reason:     summarizeViolations(violations),
		Violations: violations,
	}

	if decision == DecisionMutate && req.ToolOutput != nil {
		sanitized := *req.ToolOutput
		for _, v := range outboundVals {
			if s, ok := v.(Sanitizer); ok {
				sanitized = s.SanitizeOutput(sanitized)
			}
		}
		resp.SanitizedOutput = &sanitized
	}

	span.SetAttributes(attribute.String("decision", string(resp.Decision)))
	if resp.Decision == DecisionDeny || resp.Decision == DecisionTerminate {
		span.SetStatus(codes.Error, resp.Reason)
	}

	i.audit(req, resp, start)
	return resp
}

// ServeHTTP makes Interceptor an http.Handler.
// POST /intercept        — inbound tool call admission
// POST /validate/output  — outbound tool output validation
func (i *Interceptor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req ToolCallAdmissionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Token verification: when X-PerchGuard-Agent-Token is present and a verifier is
	// configured, validate it. Absent token = allowed (backward compatible) but
	// flagged as unregistered in the audit record.
	if i.tokenVerifier != nil {
		if token := r.Header.Get("X-PerchGuard-Agent-Token"); token != "" {
			if !i.tokenVerifier.Verify(req.SessionID, req.AgentID, token) {
				resp := &ToolCallAdmissionResponse{
					UID:           req.UID,
					Decision:      DecisionDeny,
					Reason:        "unregistered_agent: invalid or expired session token",
					PolicyMatched: "agentRegistration",
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(resp)
				return
			}
			req.Registered = true
		}
	}

	if i.manifestLookup != nil {
		req.MissionContext = i.manifestLookup(req.SessionID)
	}

	var resp *ToolCallAdmissionResponse
	if r.URL.Path == "/validate/output" {
		resp = i.InterceptOutput(r.Context(), &req)
	} else {
		resp = i.Intercept(r.Context(), &req)
	}

	w.Header().Set("Content-Type", "application/json")
	if resp.Decision == DecisionDeny || resp.Decision == DecisionTerminate {
		w.WriteHeader(http.StatusForbidden)
	}
	json.NewEncoder(w).Encode(resp)
}

// attachRisk checks whether any validator implements RiskScorer and, if so,
// sets resp.SessionRisk to the current cumulative score for this session.
func attachRisk(validators []Validator, sessionID string, resp *ToolCallAdmissionResponse) {
	for _, v := range validators {
		if rs, ok := v.(RiskScorer); ok {
			score := rs.SessionRisk(sessionID)
			resp.SessionRisk = &score
			return
		}
	}
}

// attachDrift checks whether any validator implements DriftScorer and, if so,
// sets resp.SessionDrift to this call's intent-drift score.
func attachDrift(validators []Validator, sessionID string, resp *ToolCallAdmissionResponse) {
	for _, v := range validators {
		if ds, ok := v.(DriftScorer); ok {
			drift := ds.SessionDrift(sessionID)
			resp.SessionDrift = &drift
			return
		}
	}
}

// --- helpers ---

func (i *Interceptor) denyResponse(req *ToolCallAdmissionRequest, v *PolicyViolation, d Decision) *ToolCallAdmissionResponse {
	return &ToolCallAdmissionResponse{
		UID:           req.UID,
		Decision:      d,
		Reason:        v.Detail,
		PolicyMatched: v.Policy,
		Violations:    []PolicyViolation{*v},
	}
}

func (i *Interceptor) audit(req *ToolCallAdmissionRequest, resp *ToolCallAdmissionResponse, start time.Time) {
	if i.auditLog == nil {
		return
	}
	i.mu.RLock()
	observe := i.observeMode
	i.mu.RUnlock()

	var riskScore float64
	if resp.SessionRisk != nil {
		riskScore = *resp.SessionRisk
	}

	entry := AuditEntry{
		RequestUID: req.UID,
		SessionID:  req.SessionID,
		AgentID:    req.AgentID,
		ToolName:   req.ToolCall.Name,
		Decision:   resp.Decision,
		PolicyHit:  resp.PolicyMatched,
		Reason:     resp.Reason,
		Timestamp:  start,
		DurationMs: time.Since(start).Milliseconds(),
		RiskScore:  riskScore,
		Registered: req.Registered,
		DataRefsIn: req.DataRefsIn,
		DataRefOut: resp.DataRefOut,
		DriftScore: resp.SessionDrift,
	}

	// Observe mode: record the real policy decision, then override the response to ALLOW.
	// The agent sees no blocked calls; the audit trail is complete and truthful.
	if observe && resp.Decision != DecisionAllow {
		entry.ObserveMode = true
		entry.ObservedDecision = resp.Decision
		entry.Decision = DecisionAllow
		resp.Decision = DecisionAllow
	}

	i.auditLog.Log(entry)
}

// resolveDecision picks the most severe decision across all inbound violations.
// Each violation contributes either its explicit Decision field or a severity-implied
// decision (critical/high → DENY, medium/low → HUMAN_REVIEW). The most severe result
// wins: TERMINATE > DENY > HUMAN_REVIEW.
//
// The previous first-match approach allowed a HUMAN_REVIEW from the semantic firewall
// to shadow a DENY from DataExfiltration when both fired on the same call.
func resolveDecision(violations []PolicyViolation) Decision {
	best := Decision("")
	for _, v := range violations {
		var candidate Decision
		switch {
		case v.Decision != "":
			candidate = v.Decision
		case v.Severity == "critical" || v.Severity == "high":
			candidate = DecisionDeny
		default:
			candidate = DecisionHumanReview
		}
		if decisionSeverity(candidate) > decisionSeverity(best) {
			best = candidate
		}
	}
	if best != "" {
		return best
	}
	return DecisionHumanReview
}

// decisionSeverity maps a Decision to a numeric weight for comparison.
// Higher = more severe. TERMINATE > DENY > HUMAN_REVIEW > unset.
func decisionSeverity(d Decision) int {
	switch d {
	case DecisionTerminate:
		return 4
	case DecisionDeny:
		return 3
	case DecisionHumanReview:
		return 2
	default:
		return 0
	}
}

// resolveOutboundDecision handles the return path.
// critical/high → DENY (block), medium/low → MUTATE (sanitize output).
func resolveOutboundDecision(violations []PolicyViolation) Decision {
	for _, v := range violations {
		if v.Severity == "critical" || v.Severity == "high" {
			return DecisionDeny
		}
	}
	return DecisionMutate
}

func summarizeViolations(violations []PolicyViolation) string {
	if len(violations) == 1 {
		return violations[0].Detail
	}
	return fmt.Sprintf("%d policy violations detected; first: %s", len(violations), violations[0].Detail)
}

func buildReviewContext(req *ToolCallAdmissionRequest, violations []PolicyViolation) *HumanReviewContext {
	return &HumanReviewContext{
		Summary:           fmt.Sprintf("Agent %q wants to call tool %q", req.AgentID, req.ToolCall.Name),
		RiskLevel:         violations[0].Severity,
		RecommendedAction: fmt.Sprintf("Review policy %q before allowing", violations[0].Policy),
	}
}
