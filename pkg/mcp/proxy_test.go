package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/mutator"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/quota"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/validator"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// buildInterceptor wires a real interceptor with tool-auth only (no LLM).
func buildInterceptor() *admission.Interceptor {
	p := policy.ToolAuthorizationPolicy{
		Enabled: true,
		AgentRoles: []policy.AgentRole{
			{
				Role:         "developer_agent",
				AllowedTools: []string{"read_file", "write_file", "bash"},
				DeniedTools:  []string{},
			},
		},
	}
	validators := []admission.Validator{
		validator.NewToolAuthorizationValidator(p),
	}
	return admission.NewInterceptor(validators, nil, nil, nil)
}

func toolCallRequest(id any, tool string) JSONRPCRequest {
	params, _ := json.Marshal(ToolCallParams{Name: tool, Arguments: map[string]any{"path": "/tmp/x"}})
	return JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: "tools/call", Params: params}
}

func TestProxy_NonToolCallForwarded(t *testing.T) {
	down := NewMemTransport(4)
	up := NewMemTransport(4)
	p := NewProxy(buildInterceptor(), up, down, WithAgentRole("developer_agent"))

	down.Push(JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		resp := down.Pop()
		if resp.Error != nil {
			t.Errorf("unexpected error: %v", resp.Error)
		}
		cancel()
	}()
	p.Run(ctx)
}

func TestProxy_AllowedToolForwarded(t *testing.T) {
	down := NewMemTransport(4)
	p := NewProxy(buildInterceptor(), NewMemTransport(4), down,
		WithAgentRole("developer_agent"))

	down.Push(toolCallRequest(2, "read_file"))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		resp := down.Pop()
		if resp.Error != nil {
			t.Errorf("allowed tool got error: %v", resp.Error)
		}
		cancel()
	}()
	p.Run(ctx)
}

func TestProxy_DeniedToolBlocked(t *testing.T) {
	down := NewMemTransport(4)
	p := NewProxy(buildInterceptor(), NewMemTransport(4), down,
		WithAgentRole("read_only_agent")) // role not in policy → default deny

	down.Push(toolCallRequest(3, "bash"))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		resp := down.Pop()
		if resp.Error == nil {
			t.Error("expected error response for denied tool")
		}
		if resp.Error != nil && resp.Error.Code != ErrCodeToolBlocked {
			t.Errorf("want code %d got %d", ErrCodeToolBlocked, resp.Error.Code)
		}
		cancel()
	}()
	p.Run(ctx)
}

func TestProxy_MalformedParams(t *testing.T) {
	down := NewMemTransport(4)
	p := NewProxy(buildInterceptor(), NewMemTransport(4), down,
		WithAgentRole("developer_agent"))

	down.Push(JSONRPCRequest{
		JSONRPC: "2.0", ID: 4, Method: "tools/call",
		Params: json.RawMessage(`{not valid json`),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		resp := down.Pop()
		if resp.Error == nil {
			t.Error("expected error for malformed params")
		}
		cancel()
	}()
	p.Run(ctx)
}

// captureUpstream records the last request forwarded to the upstream.
type captureUpstream struct {
	received JSONRPCRequest
}

func (c *captureUpstream) Forward(_ context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
	c.received = req
	result, _ := json.Marshal(map[string]string{"status": "ok"})
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, nil
}
func (c *captureUpstream) Close() error { return nil }

// buildMutateInterceptor wires auth + a ParameterSanitizer that strips --force.
func buildMutateInterceptor() *admission.Interceptor {
	authPolicy := policy.ToolAuthorizationPolicy{
		Enabled: true,
		AgentRoles: []policy.AgentRole{
			{Role: "developer_agent", AllowedTools: []string{"bash"}},
		},
	}
	sanitizePolicy := policy.ParameterSanitizationPolicy{
		Enabled: true,
		Rules:   []policy.SanitizationRule{{StripFlags: []string{"--force"}}},
	}
	return admission.NewInterceptor(
		[]admission.Validator{validator.NewToolAuthorizationValidator(authPolicy)},
		[]admission.Mutator{mutator.NewParameterSanitizer(sanitizePolicy)},
		nil,
		nil,
	)
}

// TestProxy_TokensFlowToQuota verifies that the proxy estimates tokens from the
// MCP payload and injects them into request metadata, which the budget checker
// then accumulates. This is the core of Copilot cost tracking.
func TestProxy_TokensFlowToQuota(t *testing.T) {
	budgetPolicy := policy.SessionBudgetPolicy{
		Enabled: true,
		Limits: policy.BudgetLimits{
			MaxTokensPerSession:    1_000_000,
			MaxCostPerSessionUSD:   100.0,
			MaxToolCallsPerSession: 100,
		},
	}
	checker := quota.NewSessionBudgetChecker(budgetPolicy, nil)

	authPolicy := policy.ToolAuthorizationPolicy{
		Enabled: true,
		AgentRoles: []policy.AgentRole{
			{Role: "developer_agent", AllowedTools: []string{"read_file"}},
		},
	}
	interceptor := admission.NewInterceptor(
		[]admission.Validator{validator.NewToolAuthorizationValidator(authPolicy)},
		nil,
		[]admission.QuotaChecker{checker},
		nil,
	)

	const sessionID = "copilot-test-session"
	down := NewMemTransport(4)
	p := NewProxy(interceptor, &captureUpstream{}, down,
		WithAgentRole("developer_agent"),
		WithSessionID(sessionID),
		WithGovernedModel("claude-sonnet-4-6"),
	)

	// Push a tool call with a meaningful payload so byte/4 estimation yields > 0.
	params, _ := json.Marshal(ToolCallParams{
		Name:      "read_file",
		Arguments: map[string]any{"path": "/workspace/src/main.go"},
	})
	down.Push(JSONRPCRequest{JSONRPC: "2.0", ID: 99, Method: "tools/call", Params: params})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		down.Pop()
		cancel()
	}()
	p.Run(ctx)

	snap, ok := checker.Snapshot(sessionID)
	if !ok {
		t.Fatal("expected budget snapshot after tool call; got none")
	}
	if snap.ToolCalls != 1 {
		t.Errorf("want 1 tool call recorded, got %d", snap.ToolCalls)
	}
	if snap.EstTokens <= 0 {
		t.Errorf("want estimated tokens > 0 (payload token injection failed), got %d", snap.EstTokens)
	}
	if snap.EstCostUSD <= 0 {
		t.Errorf("want estimated cost > 0, got %f", snap.EstCostUSD)
	}
}

func TestProxy_MutateForwardsModifiedParams(t *testing.T) {
	down := NewMemTransport(4)
	up := &captureUpstream{}
	p := NewProxy(buildMutateInterceptor(), up, down, WithAgentRole("developer_agent"))

	origParams, _ := json.Marshal(ToolCallParams{
		Name:      "bash",
		Arguments: map[string]any{"command": "git push --force origin main"},
	})
	down.Push(JSONRPCRequest{JSONRPC: "2.0", ID: 5, Method: "tools/call", Params: origParams})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		resp := down.Pop()
		if resp.Error != nil {
			t.Errorf("expected no error, got %v", resp.Error)
			cancel()
			return
		}
		// Verify upstream received the mutated params (--force stripped).
		var forwarded ToolCallParams
		if err := json.Unmarshal(up.received.Params, &forwarded); err != nil {
			t.Errorf("unmarshal forwarded params: %v", err)
			cancel()
			return
		}
		cmd, _ := forwarded.Arguments["command"].(string)
		if strings.Contains(cmd, "--force") {
			t.Errorf("expected --force to be stripped from forwarded command, got %q", cmd)
		}
		cancel()
	}()
	p.Run(ctx)
}
