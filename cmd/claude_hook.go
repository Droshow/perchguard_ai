// claude_hook.go — --mode=claude-hook
//
// Claude Code calls this subprocess for every tool call via the hooks system
// configured in ~/.claude/settings.json. The tool call arrives as JSON on stdin.
// PerchGuard admits it through the policy pipeline and signals the result via
// exit code: 0 = allow, 2 = block. Any text written to stdout is shown to the
// user when the call is blocked.
//
// Two execution paths:
//
//	Server mode  — POST to http://localhost:8080/intercept (requires server running).
//	               Auto-detected: if GET /healthz responds in < 200ms, use server mode.
//	In-process   — Load policy from disk, run pipeline inline. No server dependency.
//	               Audit written to ./snapshots/audit.jsonl.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/mutator"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/quota"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/validator"
	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
	"github.com/Droshow/PerchGuard/perchguard/pkg/envutil"
	"github.com/Droshow/PerchGuard/perchguard/pkg/llm"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// hookAuditLogger writes hook decisions to the JSONL sink.
type hookAuditLogger struct {
	push      func(store.AuditRecord)
	versionFn func() string
}

func (h *hookAuditLogger) Log(entry admission.AuditEntry) {
	h.push(store.AuditRecord{
		RequestUID:    entry.RequestUID,
		SessionID:     entry.SessionID,
		AgentID:       entry.AgentID,
		ToolName:      entry.ToolName,
		Decision:      string(entry.Decision),
		PolicyHit:     entry.PolicyHit,
		Reason:        entry.Reason,
		Timestamp:     entry.Timestamp,
		DurationMs:    entry.DurationMs,
		RiskScore:     entry.RiskScore,
		Registered:    entry.Registered,
		PolicyVersion: entry.PolicyVersion,
	})
}

type noOpAuditLogger struct{}

func (n *noOpAuditLogger) Log(_ admission.AuditEntry) {}

// claudeHookInput is the JSON structure Claude Code sends to every hook subprocess.
type claudeHookInput struct {
	SessionID     string         `json:"session_id"`
	HookEventName string         `json:"hook_event_name"` // "PreToolUse" | "PostToolUse"
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
	ToolUseID     string         `json:"tool_use_id"`
	// PostToolUse fields
	ToolResponse map[string]any `json:"tool_response,omitempty"`
	ExitCode     *int           `json:"exit_code,omitempty"`
}

// hookServerAddr is checked first; configurable via PERCHGUARD_ADDR.
const hookServerDefault = "http://localhost:8080"

// runClaudeHook is the entry point for --mode=claude-hook.
// Reads hook input from stdin, runs admission, exits 0 or 2.
func runClaudeHook(post bool) {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		// Can't read stdin — fail open (don't block Claude Code's startup).
		os.Exit(0)
	}

	var input claudeHookInput
	if err := json.Unmarshal(raw, &input); err != nil {
		os.Exit(0) // malformed input — fail open
	}

	// Empty tool name or session — nothing to govern.
	if input.ToolName == "" {
		os.Exit(0)
	}

	serverAddr := envutil.GetEnv("PERCHGUARD_ADDR", hookServerDefault)
	if !post && serverIsUp(serverAddr) {
		runHookServerMode(serverAddr, input)
		return
	}
	runHookInProcess(input, post)
}

// runHookServerMode POSTs the tool call to the running PerchGuard server and
// relays the decision via exit code.
func runHookServerMode(addr string, input claudeHookInput) {
	req := admission.ToolCallAdmissionRequest{
		SessionID: input.SessionID,
		AgentID:   "claude-code",
		AgentRole: envutil.GetEnv("PERCHGUARD_AGENT_ROLE", "developer_agent"),
		ToolCall: admission.ToolCall{
			Name:       input.ToolName,
			Parameters: input.ToolInput,
		},
		Timestamp: time.Now(),
	}

	body, _ := json.Marshal(req)
	url := addr + "/intercept"
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		os.Exit(0)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second} // 60s to allow REVIEW long-poll
	resp, err := client.Do(httpReq)
	if err != nil {
		// Server not reachable — fall back to in-process.
		runHookInProcess(input, false)
		return
	}
	defer resp.Body.Close()

	var admResp admission.ToolCallAdmissionResponse
	if err := json.NewDecoder(resp.Body).Decode(&admResp); err != nil {
		os.Exit(0)
	}

	exitFromDecision(admResp.Decision, admResp.Reason)
}

// runHookInProcess loads policy from disk and runs the admission pipeline in-process.
// Used when the server is not running (developer laptop without server mode).
func runHookInProcess(input claudeHookInput, post bool) {
	policyFile := "policies.yaml"
	policyPath := findPolicyPath(policyFile)
	if policyPath == "" {
		// No policy found — fail open and warn.
		fmt.Fprintf(os.Stderr, "[perchguard] no policy file found — run perchguard init\n")
		os.Exit(0)
	}

	policyResult, err := policy.LoadWithMeta(policyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perchguard] policy load error: %v\n", err)
		os.Exit(0)
	}
	cfg := policyResult.Config

	// Minimal in-process pipeline: stateless validators + mutators.
	// Fleet manager and semantic firewall are skipped unless LLM key is set.
	// Session budget uses a per-process in-memory store (no cross-call state).
	memStore := store.NewMemoryStore()
	budgetChecker := quota.NewSessionBudgetChecker(cfg.Policies.SessionBudget, memStore)

	var llmClient llm.Client
	if key := os.Getenv("PERCHGUARD_LLM_API_KEY"); key != "" {
		model := envutil.GetEnv("PERCHGUARD_LLM_MODEL", cfg.Policies.SemanticFirewall.LLM.Model)
		if model == "" {
			model = "claude-haiku-4-5"
		}
		llmClient = llm.NewClaudeClient(key, llm.WithModel(model))
	}

	validators := []admission.Validator{
		validator.NewPromptInjectionValidator(cfg.Policies.PromptInjection),
		validator.NewToolAuthorizationValidator(cfg.Policies.ToolAuthorization),
		validator.NewDataExfiltrationValidator(cfg.Policies.DataExfiltration),
	}
	if cfg.Policies.SemanticFirewall.Enabled && llmClient != nil {
		validators = append(validators, validator.NewSemanticFirewallValidator(cfg.Policies.SemanticFirewall, llmClient))
	}

	mutators := []admission.Mutator{
		mutator.NewParameterSanitizer(cfg.Policies.ParameterSanitization),
		mutator.NewLeastPrivilegeMutator(cfg.Policies.LeastPrivilege),
	}
	quotas := []admission.QuotaChecker{
		quota.NewDepthLimiter(cfg.Policies.DepthLimiter),
		budgetChecker,
	}

	// Audit: chain JSONL + SQLite. Either can fail independently.
	var pushFn func(store.AuditRecord)

	jsonlSink, jsonlErr := audit.NewJSONLRecordSink("./snapshots/audit.jsonl")
	if jsonlErr == nil {
		pushFn = jsonlSink.Push
	}

	dbPath := envutil.GetEnv("PERCHGUARD_DB_PATH", store.DefaultDBPath())
	if sqliteSink, err := store.NewSQLiteAuditSink(dbPath); err == nil {
		prev := pushFn
		pushFn = func(rec store.AuditRecord) {
			if prev != nil {
				prev(rec)
			}
			sqliteSink.Push(rec)
		}
	}

	var auditLog admission.AuditLogger
	if pushFn != nil {
		auditLog = &hookAuditLogger{push: pushFn, versionFn: func() string { return policyResult.Hash }}
	} else {
		auditLog = &noOpAuditLogger{}
	}

	interceptor := admission.NewInterceptor(validators, mutators, quotas, auditLog,
		admission.WithObserveMode(cfg.IsObserveMode()),
		admission.WithOutboundValidators(validator.NewOutputValidator(cfg.Policies.OutputValidation)),
	)

	ctx := context.Background()

	if post {
		// PostToolUse — validate tool output.
		output := extractToolOutput(input.ToolResponse)
		if output == "" {
			os.Exit(0)
		}
		req := &admission.ToolCallAdmissionRequest{
			SessionID: input.SessionID,
			AgentID:   "claude-code",
			AgentRole: envutil.GetEnv("PERCHGUARD_AGENT_ROLE", "developer_agent"),
			ToolCall: admission.ToolCall{
				Name:       input.ToolName,
				Parameters: input.ToolInput,
			},
			ToolOutput: &output,
			Timestamp:  time.Now(),
		}
		resp := interceptor.InterceptOutput(ctx, req)
		exitFromDecision(resp.Decision, resp.Reason)
		return
	}

	req := &admission.ToolCallAdmissionRequest{
		SessionID: input.SessionID,
		AgentID:   "claude-code",
		AgentRole: envutil.GetEnv("PERCHGUARD_AGENT_ROLE", "developer_agent"),
		ToolCall: admission.ToolCall{
			Name:       input.ToolName,
			Parameters: input.ToolInput,
		},
		Timestamp: time.Now(),
	}
	resp := interceptor.Intercept(ctx, req)
	exitFromDecision(resp.Decision, resp.Reason)
}

// exitFromDecision maps an admission decision to a Claude Code hook exit code.
// 0 = allow, 2 = block (Claude Code reads stdout for the block message).
func exitFromDecision(decision admission.Decision, reason string) {
	switch decision {
	case admission.DecisionDeny, admission.DecisionTerminate:
		fmt.Printf("PerchGuard blocked this tool call: %s\n", reason)
		os.Exit(2)
	case admission.DecisionHumanReview:
		// In in-process mode, human review falls back to deny (no server to long-poll).
		fmt.Printf("PerchGuard requires human review (no server running): %s\n", reason)
		os.Exit(2)
	default:
		os.Exit(0)
	}
}

// serverIsUp checks whether the PerchGuard server is reachable in < 200ms.
func serverIsUp(addr string) bool {
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get(addr + "/healthz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// extractToolOutput pulls the text content out of a Claude tool_result response.
func extractToolOutput(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	// Claude tool_result: {"type":"tool_result","content":"..."}
	// or content is an array of content blocks
	switch v := resp["content"].(type) {
	case string:
		return v
	case []any:
		var out string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					out += t
				}
			}
		}
		return out
	}
	// Fallback: marshal the whole response as the output string.
	b, _ := json.Marshal(resp)
	return string(b)
}

// Minimal audit loggers used in in-process mode to avoid importing the full telemetry stack.

func init() {
	// Silence the standard logger in hook mode — output goes to Claude Code's terminal.
	log.SetOutput(io.Discard)
}
