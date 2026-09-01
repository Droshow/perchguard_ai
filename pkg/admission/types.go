// Package admission defines the core types for PerchGuard's agentic admission control.
//
// These types are the agentic equivalent of the K8s AdmissionReview/AdmissionResponse
// pattern used in EKS-BankingKube/Dynamic_Pod_Sec.
//
// Mapping:
//
//	K8s AdmissionReview     → ToolCallAdmissionRequest
//	K8s AdmissionResponse   → ToolCallAdmissionResponse
//	K8s Pod spec            → ToolCall
//	kubectl apply           → agent.execute_tool()
package admission

import (
	"context"
	"time"
)

// Decision is the outcome of the admission pipeline.
// Mirrors the K8s Allowed/Denied pattern but with richer semantics.
type Decision string

const (
	DecisionAllow       Decision = "ALLOW"        // Tool call is safe - proceed
	DecisionDeny        Decision = "DENY"         // Tool call is blocked - return error to agent
	DecisionMutate      Decision = "MUTATE"       // Tool call allowed but parameters were modified
	DecisionHumanReview Decision = "HUMAN_REVIEW" // Escalate to human - pause agent execution
	DecisionTerminate   Decision = "TERMINATE"    // Kill the session (budget exceeded, runaway loop)
)

// ToolCall represents a single tool invocation proposed by the agent.
// Analogous to the corev1.Pod spec that K8s admission controllers inspect.
type ToolCall struct {
	// Name is the tool identifier, e.g. "bash", "write_file", "database:query"
	Name string `json:"name"`
	// Parameters are the tool's arguments - this is the primary inspection surface
	Parameters map[string]any `json:"parameters"`
	// Source identifies which MCP server or tool framework owns this tool
	Source string `json:"source,omitempty"`
	// DestinationURL is set for network-bound tools (HTTP, webhooks, etc.)
	DestinationURL string `json:"destination_url,omitempty"`
}

// Message represents one turn in the conversation history.
type Message struct {
	Role    string `json:"role"` // "user" | "assistant" | "tool"
	Content string `json:"content"`
}

// MissionContext carries the registered agent's declared mission into the admission pipeline.
// Populated from the manifest store by the interceptor when the session is registered.
// Nil when the agent is unregistered.
type MissionContext struct {
	Summary    string   `json:"summary"`
	Scope      []string `json:"scope"`
	OutOfScope []string `json:"out_of_scope"`
}

// ToolCallAdmissionRequest is the payload PerchGuard receives for every tool call.
// Analogous to admissionv1.AdmissionReview in Dynamic_Pod_Sec.
type ToolCallAdmissionRequest struct {
	// UID is a unique identifier for this specific admission request.
	// Used to correlate requests and responses in audit logs.
	UID string `json:"uid"`

	// SessionID groups all tool calls from a single agent conversation.
	// Used by the quota layer to track cumulative usage.
	SessionID string `json:"session_id"`

	// AgentID identifies the agent making the call.
	// Used for role-based tool authorization (agentRoles in policies.yaml).
	AgentID string `json:"agent_id"`

	// AgentRole maps to the authorization policy (e.g. "read_only_agent", "developer_agent")
	AgentRole string `json:"agent_role"`

	// UserIntent is the original user request - used by the semantic firewall to
	// check that the proposed tool call actually serves what the user asked for.
	UserIntent string `json:"user_intent"`

	// Conversation is the full conversation history up to this point.
	// Gives the validator full context to detect prompt injection in prior turns.
	Conversation []Message `json:"conversation"`

	// ToolCall is the action the agent wants to take - the primary admission subject.
	ToolCall ToolCall `json:"tool_call"`

	// ToolOutput is set on the RETURN path (outbound admission).
	// PerchGuard can validate what the tool returned before feeding it back to the agent.
	ToolOutput *string `json:"tool_output,omitempty"`

	// NestingDepth tracks how many layers of agent-calling-agent recursion we're in.
	NestingDepth int `json:"nesting_depth"`

	// DataRefsIn lists the lineage refs (from prior DataRefOut values) that this call
	// is consuming as inputs. Used by the lineage validator to detect cross-boundary flows.
	DataRefsIn []string `json:"data_refs_in,omitempty"`

	// Metadata carries arbitrary context (user ID, tenant ID, environment, etc.)
	Metadata map[string]string `json:"metadata,omitempty"`

	// Timestamp of when the request was received by PerchGuard.
	Timestamp time.Time `json:"timestamp"`

	// Registered is set by the HTTP layer: true when the caller presented a valid
	// X-PerchGuard-Agent-Token matching the session's manifest registration.
	// False for unregistered (token-less) agents — they are still admitted but
	// flagged in the audit record.
	Registered bool `json:"-"`

	// MissionContext is populated from the agent's registered manifest.
	// Used by the semantic firewall to check intent against declared scope.
	// Not serialised — set internally by the interceptor, not by callers.
	MissionContext *MissionContext `json:"-"`
}

// ToolCallAdmissionResponse is PerchGuard's verdict on the admission request.
// Analogous to admissionv1.AdmissionResponse in Dynamic_Pod_Sec.
type ToolCallAdmissionResponse struct {
	UID                string              `json:"uid"`
	Decision           Decision            `json:"decision"`
	MutatedCall        *ToolCall           `json:"mutated_call,omitempty"`
	Reason             string              `json:"reason"`
	PolicyMatched      string              `json:"policy_matched,omitempty"`
	Violations         []PolicyViolation   `json:"violations,omitempty"`
	HumanReviewContext *HumanReviewContext `json:"human_review_context,omitempty"`
	// SanitizedOutput is set on the outbound path when Decision == DecisionMutate.
	// The agent should use this cleaned output instead of the raw tool result.
	SanitizedOutput *string `json:"sanitized_output,omitempty"`
	// SessionRisk is the cumulative 0.0–1.0 fleet risk score for this session after
	// this call. Nil when no fleet validator is active. Callers can use this to make
	// adaptive decisions (slow down, alert, escalate) without polling GET /api/audit.
	SessionRisk *float64 `json:"session_risk,omitempty"`

	// SessionDrift is this call's cosine drift (0.0-1.0) against the session's intent
	// baseline. Nil when no fleet validator is active. Threaded through to the audit
	// trail so per-call drift survives session deletion (drift-timeline report).
	SessionDrift *float64 `json:"session_drift,omitempty"`

	// DataRefOut is the lineage ref assigned to this call's output. Include it in
	// DataRefsIn on any subsequent call that consumes the result of this one.
	DataRefOut string `json:"data_ref_out,omitempty"`
}

// RiskScorer is implemented by validators that track per-session cumulative risk.
// The interceptor checks validators for this interface after each pipeline run and
// populates ToolCallAdmissionResponse.SessionRisk when one is found.
type RiskScorer interface {
	SessionRisk(sessionID string) float64
}

// DriftScorer is implemented by validators that track per-call intent drift.
// The interceptor checks validators for this interface after each pipeline run and
// populates ToolCallAdmissionResponse.SessionDrift when one is found.
type DriftScorer interface {
	SessionDrift(sessionID string) float64
}

// OutboundValidator is a Validator that only runs on the return path (tool output).
// Validators implementing this are skipped in Intercept() and only called in InterceptOutput().
type OutboundValidator interface {
	Validator
	IsOutbound() bool
}

// Sanitizer is implemented by OutboundValidators that can clean tool outputs.
// The interceptor calls SanitizeOutput when the outbound decision is MUTATE.
type Sanitizer interface {
	SanitizeOutput(output string) string
}

// ReviewDispatcher sends HUMAN_REVIEW requests to an external webhook.
// Defined as an interface here to avoid import cycles with pkg/humanreview.
type ReviewDispatcher interface {
	Dispatch(ctx context.Context, req ReviewRequest) (approved bool, err error)
	TimeoutSeconds() int
}

// ReviewRequest is the payload sent to a human review webhook.
type ReviewRequest struct {
	RequestUID string            `json:"request_uid"`
	SessionID  string            `json:"session_id"`
	AgentID    string            `json:"agent_id"`
	ToolCall   ToolCall          `json:"tool_call"`
	UserIntent string            `json:"user_intent"`
	Violations []PolicyViolation `json:"violations"`
	Summary    string            `json:"summary"`
}

// ReviewResponse is the response from the human review webhook.
type ReviewResponse struct {
	RequestUID string `json:"request_uid"`
	Approved   bool   `json:"approved"`
	Reason     string `json:"reason,omitempty"`
	ReviewerID string `json:"reviewer_id,omitempty"`
}

// PolicyViolation describes a single policy rule that was triggered.
type PolicyViolation struct {
	Layer    string   `json:"layer"`              // "validation" | "mutation" | "quota" | "outbound"
	Policy   string   `json:"policy"`             // policy name from policies.yaml
	Detail   string   `json:"detail"`             // specific violation description
	Severity string   `json:"severity"`           // "critical" | "high" | "medium" | "low"
	Decision Decision `json:"decision,omitempty"` // explicit decision override; "" = derive from Severity
}

// HumanReviewContext gives the human reviewer everything they need to make a decision.
type HumanReviewContext struct {
	// Summary is a one-sentence description of what the agent wants to do.
	Summary string `json:"summary"`
	// RiskLevel is an estimated risk: "critical" | "high" | "medium" | "low"
	RiskLevel string `json:"risk_level"`
	// RecommendedAction is what PerchGuard thinks the human should do.
	RecommendedAction string `json:"recommended_action"`
}

// AuditEntry is written to the audit log for every admission decision.
// OpenTelemetry spans are built from these entries.
type AuditEntry struct {
	RequestUID       string    `json:"request_uid"`
	SessionID        string    `json:"session_id"`
	AgentID          string    `json:"agent_id"`
	ToolName         string    `json:"tool_name"`
	Decision         Decision  `json:"decision"`
	PolicyHit        string    `json:"policy_hit,omitempty"`
	Reason           string    `json:"reason"`
	Timestamp        time.Time `json:"timestamp"`
	DurationMs       int64     `json:"duration_ms"`
	RiskScore        float64   `json:"risk_score"`     // cumulative session risk at the moment of this decision
	Registered       bool      `json:"registered"`     // false when no valid agent token was presented
	PolicyVersion    string    `json:"policy_version"` // sha256[:8] of policies.yaml in effect at decision time
	DataRefsIn       []string  `json:"data_refs_in,omitempty"`
	DataRefOut       string    `json:"data_ref_out,omitempty"`
	ObserveMode      bool      `json:"observe_mode,omitempty"`
	ObservedDecision Decision  `json:"observed_decision,omitempty"`
	DriftScore       *float64  `json:"drift_score,omitempty"` // intent drift for this call; nil when no fleet validator scored it
}
