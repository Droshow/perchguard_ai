package validator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/llm"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

type mockLLM struct {
	resp  llm.CompletionResponse
	err   error
	sleep time.Duration
	calls int
}

func (m *mockLLM) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	m.calls++
	if m.sleep > 0 {
		select {
		case <-time.After(m.sleep):
		case <-ctx.Done():
			return llm.CompletionResponse{}, ctx.Err()
		}
	}
	return m.resp, m.err
}

func sfPolicy(enabled bool, threshold float64) policy.SemanticFirewallPolicy {
	return policy.SemanticFirewallPolicy{
		Enabled:                  enabled,
		IntentAlignmentThreshold: threshold,
		LLM: policy.LLMConfig{
			MaxTokens:      50,
			SkipIfNoIntent: true,
			BudgetMs:       500,
		},
	}
}

func TestSemanticFirewallValidator(t *testing.T) {
	tests := []struct {
		name      string
		policy    policy.SemanticFirewallPolicy
		req       *admission.ToolCallAdmissionRequest
		mock      *mockLLM
		wantViol  bool
		wantCalls int
	}{
		{
			name:   "disabled — skips LLM",
			policy: sfPolicy(false, 0.6),
			req:    &admission.ToolCallAdmissionRequest{UserIntent: "read file"},
			mock:   &mockLLM{},
		},
		{
			name:   "empty intent with skipIfNoIntent",
			policy: sfPolicy(true, 0.6),
			req:    &admission.ToolCallAdmissionRequest{UserIntent: ""},
			mock:   &mockLLM{},
		},
		{
			name:   "aligned call — no violation",
			policy: sfPolicy(true, 0.6),
			req: &admission.ToolCallAdmissionRequest{
				UserIntent: "read config",
				ToolCall:   admission.ToolCall{Name: "read_file"},
			},
			mock:      &mockLLM{resp: llm.CompletionResponse{Content: `{"aligned":true,"confidence":0.95,"reason":"matches"}`}},
			wantCalls: 1,
		},
		{
			name:   "diverged above threshold — violation",
			policy: sfPolicy(true, 0.6),
			req: &admission.ToolCallAdmissionRequest{
				UserIntent: "read config",
				ToolCall:   admission.ToolCall{Name: "bash"},
			},
			mock:      &mockLLM{resp: llm.CompletionResponse{Content: `{"aligned":false,"confidence":0.85,"reason":"bash unneeded"}`}},
			wantViol:  true,
			wantCalls: 1,
		},
		{
			name:   "diverged but below threshold — no violation",
			policy: sfPolicy(true, 0.6),
			req: &admission.ToolCallAdmissionRequest{
				UserIntent: "do something",
				ToolCall:   admission.ToolCall{Name: "bash"},
			},
			mock:      &mockLLM{resp: llm.CompletionResponse{Content: `{"aligned":false,"confidence":0.4,"reason":"uncertain"}`}},
			wantCalls: 1,
		},
		{
			name:   "LLM error — fail open",
			policy: sfPolicy(true, 0.6),
			req:    &admission.ToolCallAdmissionRequest{UserIntent: "x", ToolCall: admission.ToolCall{Name: "bash"}},
			mock:   &mockLLM{err: errors.New("api down")},
			wantCalls: 1,
		},
		{
			name:   "LLM timeout — fail open",
			policy: sfPolicy(true, 0.6),
			req:    &admission.ToolCallAdmissionRequest{UserIntent: "x", ToolCall: admission.ToolCall{Name: "bash"}},
			mock:   &mockLLM{sleep: 600 * time.Millisecond}, // exceeds 500ms budget
			wantCalls: 1,
		},
		{
			name:   "invalid JSON — fail open",
			policy: sfPolicy(true, 0.6),
			req:    &admission.ToolCallAdmissionRequest{UserIntent: "x", ToolCall: admission.ToolCall{Name: "bash"}},
			mock:   &mockLLM{resp: llm.CompletionResponse{Content: "not json at all"}},
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewSemanticFirewallValidator(tt.policy, tt.mock)
			viol := v.Validate(context.Background(), tt.req)
			if (viol != nil) != tt.wantViol {
				t.Errorf("wantViol=%v got violation=%v", tt.wantViol, viol)
			}
			if tt.mock.calls != tt.wantCalls {
				t.Errorf("LLM calls: want %d got %d", tt.wantCalls, tt.mock.calls)
			}
			if viol != nil {
				if viol.Severity != "medium" {
					t.Errorf("expected severity medium, got %q", viol.Severity)
				}
			}
		})
	}
}
