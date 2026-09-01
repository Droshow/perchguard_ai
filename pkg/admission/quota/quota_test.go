package quota

import (
	"context"
	"strconv"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

func newBudgetPolicy(maxCalls int, maxTokens int, maxCost float64) policy.SessionBudgetPolicy {
	return policy.SessionBudgetPolicy{
		Enabled: true,
		Limits: policy.BudgetLimits{
			MaxToolCallsPerSession: maxCalls,
			MaxTokensPerSession:    maxTokens,
			MaxCostPerSessionUSD:   maxCost,
		},
	}
}

func reqWithTokens(sessionID, model string, tokens int) *admission.ToolCallAdmissionRequest {
	return &admission.ToolCallAdmissionRequest{
		SessionID: sessionID,
		ToolCall:  admission.ToolCall{Name: "read_file"},
		Metadata: map[string]string{
			"tokens_used": strconv.Itoa(tokens),
			"model":       model,
		},
	}
}

func TestSessionBudgetChecker_Snapshot_Unknown(t *testing.T) {
	c := NewSessionBudgetChecker(newBudgetPolicy(100, 1_000_000, 10.0), nil)
	if _, ok := c.Snapshot("nonexistent"); ok {
		t.Error("expected false for unknown session, got true")
	}
}

func TestSessionBudgetChecker_Snapshot_AfterRecords(t *testing.T) {
	c := NewSessionBudgetChecker(newBudgetPolicy(100, 1_000_000, 10.0), nil)
	req := reqWithTokens("s1", "claude-sonnet-4-6", 1000)
	ctx := context.Background()

	c.Record(ctx, req)
	c.Record(ctx, req)

	snap, ok := c.Snapshot("s1")
	if !ok {
		t.Fatal("expected snapshot, got false")
	}
	if snap.ToolCalls != 2 {
		t.Errorf("want 2 tool calls, got %d", snap.ToolCalls)
	}
	if snap.EstTokens != 2000 {
		t.Errorf("want 2000 estimated tokens, got %d", snap.EstTokens)
	}
	if snap.EstCostUSD <= 0 {
		t.Errorf("want cost > 0, got %f", snap.EstCostUSD)
	}
	if snap.Limits.MaxToolCallsPerSession != 100 {
		t.Errorf("limits not propagated into snapshot")
	}
}

func TestSessionBudgetChecker_ReloadPolicy(t *testing.T) {
	original := newBudgetPolicy(100, 1_000_000, 10.0)
	c := NewSessionBudgetChecker(original, nil)
	req := &admission.ToolCallAdmissionRequest{SessionID: "s2", ToolCall: admission.ToolCall{Name: "bash"}}
	ctx := context.Background()

	// Exhaust call limit to 1.
	tighter := newBudgetPolicy(1, 1_000_000, 10.0)
	c.ReloadPolicy(tighter)
	c.Record(ctx, req) // first call — now at limit

	v := c.Check(ctx, req)
	if v == nil {
		t.Error("expected violation after reload tightened limits, got nil")
	}
}

func TestSessionBudgetChecker_CostCapEnforced(t *testing.T) {
	// Set a very low cost cap so one expensive call trips it.
	p := newBudgetPolicy(100, 1_000_000, 0.001) // $0.001 cap
	c := NewSessionBudgetChecker(p, nil)
	req := reqWithTokens("s3", "claude-opus-4-7", 10000) // expensive model, many tokens
	ctx := context.Background()

	c.Record(ctx, req) // accumulates cost well above $0.001

	v := c.Check(ctx, req)
	if v == nil {
		t.Error("expected cost violation, got nil")
	}
	if v != nil && v.Policy != "sessionBudget.maxCost" {
		t.Errorf("want policy sessionBudget.maxCost, got %s", v.Policy)
	}
}

func TestDepthLimiter(t *testing.T) {
	tests := []struct {
		name    string
		policy  policy.DepthLimiterPolicy
		depth   int
		wantNil bool
	}{
		{
			name:    "under limit ALLOW",
			policy:  policy.DepthLimiterPolicy{Enabled: true, MaxAgentNestingDepth: 3},
			depth:   2,
			wantNil: true,
		},
		{
			name:    "at limit DENY",
			policy:  policy.DepthLimiterPolicy{Enabled: true, MaxAgentNestingDepth: 3},
			depth:   3,
			wantNil: false,
		},
		{
			name:    "over limit DENY",
			policy:  policy.DepthLimiterPolicy{Enabled: true, MaxAgentNestingDepth: 3},
			depth:   5,
			wantNil: false,
		},
		{
			name:    "disabled always ALLOW",
			policy:  policy.DepthLimiterPolicy{Enabled: false, MaxAgentNestingDepth: 1},
			depth:   99,
			wantNil: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			d := NewDepthLimiter(tc.policy)
			req := &admission.ToolCallAdmissionRequest{
				SessionID:    "s1",
				NestingDepth: tc.depth,
			}
			v := d.Check(context.Background(), req)
			if tc.wantNil && v != nil {
				t.Errorf("want nil violation, got %+v", v)
			}
			if !tc.wantNil && v == nil {
				t.Error("want violation, got nil")
			}
		})
	}
}

func TestSessionBudgetChecker(t *testing.T) {
	tests := []struct {
		name      string
		policy    policy.SessionBudgetPolicy
		preRecord int // call Record this many times before Check
		wantNil   bool
	}{
		{
			name: "under budget ALLOW",
			policy: policy.SessionBudgetPolicy{
				Enabled: true,
				Limits: policy.BudgetLimits{
					MaxTokensPerSession:    100000,
					MaxCostPerSessionUSD:   10.0,
					MaxToolCallsPerSession: 50,
				},
			},
			preRecord: 0,
			wantNil:   true,
		},
		{
			name: "at call limit DENY",
			policy: policy.SessionBudgetPolicy{
				Enabled: true,
				Limits: policy.BudgetLimits{
					MaxTokensPerSession:    100000,
					MaxCostPerSessionUSD:   10.0,
					MaxToolCallsPerSession: 2,
				},
			},
			preRecord: 2, // exhaust the limit
			wantNil:   false,
		},
		{
			name: "disabled always ALLOW",
			policy: policy.SessionBudgetPolicy{
				Enabled: false,
				Limits:  policy.BudgetLimits{MaxToolCallsPerSession: 1},
			},
			preRecord: 5,
			wantNil:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := NewSessionBudgetChecker(tc.policy, nil)
			req := &admission.ToolCallAdmissionRequest{
				SessionID: "session-abc",
				ToolCall:  admission.ToolCall{Name: "read_file"},
			}
			for range make([]struct{}, tc.preRecord) {
				c.Record(context.Background(), req)
			}
			v := c.Check(context.Background(), req)
			if tc.wantNil && v != nil {
				t.Errorf("want nil violation, got %+v", v)
			}
			if !tc.wantNil && v == nil {
				t.Error("want violation, got nil")
			}
		})
	}
}
