package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/llm"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/telemetry"
)

// SemanticFirewallValidator calls an LLM to verify that the proposed tool call
// actually serves the user's stated intent. Returns a medium-severity violation
// (→ HUMAN_REVIEW) when the model is confident the call is misaligned.
//
// Critical behaviour: always fails open. LLM timeout, network error, or JSON
// parse failure all return nil — the pipeline must not be blocked by LLM
// unavailability.
type SemanticFirewallValidator struct {
	policy policy.SemanticFirewallPolicy
	client llm.Client
}

func NewSemanticFirewallValidator(p policy.SemanticFirewallPolicy, client llm.Client) *SemanticFirewallValidator {
	return &SemanticFirewallValidator{policy: p, client: client}
}

func (v *SemanticFirewallValidator) Name() string { return "semantic_firewall" }

func (v *SemanticFirewallValidator) Validate(ctx context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	if !v.policy.Enabled {
		return nil
	}
	if req.UserIntent == "" && v.policy.LLM.SkipIfNoIntent {
		return nil
	}

	// Always use a fresh context for the LLM call — detached from the request context.
	// The semantic firewall has a separate time budget: the HTTP request deadline may be
	// shorter than the LLM call requires, and a timeout here is indistinguishable from
	// ALLOW (fail-open). Give it the full configured budget every time.
	budgetMs := v.policy.LLM.BudgetMs
	if budgetMs <= 0 {
		budgetMs = 10000
	}
	llmCtx, cancel := context.WithTimeout(context.Background(), time.Duration(budgetMs)*time.Millisecond)
	defer cancel()
	_ = ctx // request ctx kept for future cancellation propagation if needed

	// Build mission context block from the registered manifest when available.
	// Without it the firewall can only check tool-intent coherence, not mission alignment.
	missionBlock := "No declared mission on file for this agent."
	if mc := req.MissionContext; mc != nil {
		missionBlock = fmt.Sprintf("Mission: %s", mc.Summary)
		if len(mc.Scope) > 0 {
			missionBlock += fmt.Sprintf("\nAuthorized scope: %s", strings.Join(mc.Scope, ", "))
		}
		if len(mc.OutOfScope) > 0 {
			missionBlock += fmt.Sprintf("\nExplicitly out of scope: %s", strings.Join(mc.OutOfScope, ", "))
		}
	}

	systemPrompt := `You are a security validator for an AI agent system.` +
		` Your task is to determine whether the user's stated intent aligns with the agent's declared mission.` +
		` Aligned means the intent is consistent with the authorized scope.` +
		` Not aligned means the intent is outside the mission, violates constraints, or could cause harm beyond the declared purpose.` +
		` Reply ONLY with valid JSON: {"aligned":true/false,"confidence":0.0-1.0,"reason":"brief"}`

	userMessage := fmt.Sprintf("%s\n\nUser intent: %q\nProposed tool: %q",
		missionBlock, req.UserIntent, req.ToolCall.Name)

	sfStart := time.Now()
	resp, err := v.client.Complete(llmCtx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		UserMessage:  userMessage,
		MaxTokens:    v.policy.LLM.MaxTokens,
		Temperature:  v.policy.LLM.Temperature,
		Model:        v.policy.LLM.Model,
	})
	telemetry.SemanticFirewallDuration.Observe(time.Since(sfStart).Seconds())
	if err != nil {
		return nil // fail open
	}

	var result struct {
		Aligned    bool    `json:"aligned"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	// Extract the JSON object regardless of surrounding markdown fences or whitespace.
	content := resp.Content
	if i, j := strings.Index(content, "{"), strings.LastIndex(content, "}"); i >= 0 && j > i {
		content = content[i : j+1]
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil // fail open on parse error
	}
	threshold := v.policy.IntentAlignmentThreshold

	if !result.Aligned && result.Confidence >= threshold {
		return &admission.PolicyViolation{
			Layer:    "validation",
			Policy:   "semanticFirewall.intentAlignment",
			Detail:   fmt.Sprintf("intent divergence (confidence %.2f): %s", result.Confidence, result.Reason),
			Severity: "medium",
		}
	}
	return nil
}
