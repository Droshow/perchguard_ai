// Package mutator contains PerchGuard's mutation steps.
//
// Analogy to EKS-BankingKube:
//   mutatePod() / applyBaselineSecurity() in webhook.go
//   → These mutators transform tool call parameters to be safer,
//     just as applyBaselineSecurity() adds runAsNonRoot, drops CAP_SYS_ADMIN, etc.
//
// Key insight: Mutation is the "Safety Net" - the agent's request is mostly fine
// but needs a nudge. We don't block, we transform.
package mutator

import (
	"context"
	"fmt"
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// ParameterSanitizer applies safe transformations to tool call parameters.
//
// Examples:
//   "rm -rf /tmp/build"     → "rm -rf /tmp/build --dry-run"  (inject dry-run)
//   "git push --force main" → "git push main"                 (strip --force)
//   "bash:rm -rf /"         → DENY (caught by validator first, but belt-and-suspenders)
//
// Mirrors: applyBaselineSecurity() which adds runAsNonRoot, drops CAP_SYS_ADMIN.
type ParameterSanitizer struct {
	policy policy.ParameterSanitizationPolicy
}

func NewParameterSanitizer(p policy.ParameterSanitizationPolicy) *ParameterSanitizer {
	return &ParameterSanitizer{policy: p}
}

func (m *ParameterSanitizer) Name() string { return "parameter_sanitizer" }

func (m *ParameterSanitizer) Mutate(ctx context.Context, req *admission.ToolCallAdmissionRequest) (*admission.ToolCall, error) {
	if !m.policy.Enabled {
		return nil, nil
	}

	mutated := false
	call := cloneToolCall(req.ToolCall)

	for _, rule := range m.policy.Rules {
		// Does this rule apply to this tool? Case-insensitive to handle
		// tools like "Bash" matching policy rule "bash".
		if rule.Match != "" && !strings.HasPrefix(strings.ToLower(call.Name), strings.ToLower(rule.Match)) {
			continue
		}

		// Check if the "unless" session flag is set (opt-out for trusted callers)
		if rule.Unless != "" {
			if val, ok := req.Metadata[rule.Unless]; ok && val == "true" {
				continue
			}
		}

		// Apply: inject a flag if the command contains dangerous substrings.
		// Checks both "command" (bash) and "query" (run_sql) parameter keys.
		if len(rule.ContainsAny) > 0 && rule.Inject != "" {
			for _, paramKey := range []string{"command", "query"} {
				if val, ok := call.Parameters[paramKey].(string); ok {
					if commandContainsAny(val, rule.ContainsAny) && !strings.Contains(val, rule.Inject) {
						call.Parameters[paramKey] = val + " " + rule.Inject
						mutated = true
					}
				}
			}
		}

		// Apply: strip dangerous flags (command and query params)
		for _, flag := range rule.StripFlags {
			for _, paramKey := range []string{"command", "query"} {
				if val, ok := call.Parameters[paramKey].(string); ok {
					if strings.Contains(val, flag) {
						call.Parameters[paramKey] = strings.ReplaceAll(val, flag, "")
						mutated = true
					}
				}
			}
		}
	}

	if !mutated {
		return nil, nil
	}
	return &call, nil
}

// --- Helpers ---

// cloneToolCall creates a deep copy so we don't mutate the original request.
func cloneToolCall(tc admission.ToolCall) admission.ToolCall {
	clone := admission.ToolCall{
		Name:           tc.Name,
		Source:         tc.Source,
		DestinationURL: tc.DestinationURL,
		Parameters:     make(map[string]any, len(tc.Parameters)),
	}
	for k, v := range tc.Parameters {
		clone.Parameters[k] = v
	}
	return clone
}

func getCommandParam(tc admission.ToolCall) string {
	if cmd, ok := tc.Parameters["command"].(string); ok {
		return cmd
	}
	return ""
}

func commandContainsAny(cmd string, patterns []string) bool {
	lower := strings.ToLower(cmd)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// LeastPrivilegeMutator enforces scope constraints on tool call parameters.
//
// Examples:
//   SQL: "SELECT * FROM accounts" → "SELECT * FROM accounts WHERE user_id = 'u123'"
//   File: write_file("/etc/passwd") → DENY (validator catches this, but also scoped here)
//   File: write_file("../../secrets") → normalized to "/workspace/secrets"
//
// Mirrors: how applyBaselineSecurity() enforces readOnlyRootFilesystem, drops capabilities.
type LeastPrivilegeMutator struct {
	policy    policy.LeastPrivilegePolicy
	sessionFn func(sessionID string) map[string]string // returns session context
}

func NewLeastPrivilegeMutator(p policy.LeastPrivilegePolicy) *LeastPrivilegeMutator {
	return &LeastPrivilegeMutator{policy: p}
}

func (m *LeastPrivilegeMutator) Name() string { return "least_privilege" }

func (m *LeastPrivilegeMutator) Mutate(ctx context.Context, req *admission.ToolCallAdmissionRequest) (*admission.ToolCall, error) {
	if !m.policy.Enabled {
		return nil, nil
	}

	call := cloneToolCall(req.ToolCall)
	mutated := false

	// File path traversal prevention
	if m.policy.FileOperations.BlockPathTraversal {
		if path, ok := call.Parameters["path"].(string); ok {
			if strings.Contains(path, "..") {
				return nil, fmt.Errorf("path traversal attempt blocked: %q", path)
			}
		}
	}

	// Enforce allowed base paths
	if len(m.policy.FileOperations.AllowedBasePaths) > 0 {
		if path, ok := call.Parameters["path"].(string); ok {
			allowed := false
			for _, base := range m.policy.FileOperations.AllowedBasePaths {
				if strings.HasPrefix(path, base) {
					allowed = true
					break
				}
			}
			if !allowed && m.policy.FileOperations.BlockAbsolutePaths && strings.HasPrefix(path, "/") {
				return nil, fmt.Errorf("absolute path %q is not in allowed base paths", path)
			}
		}
	}

	// SQL: auto-append user scope WHERE clause
	if m.policy.SQLInjection.AutoAppendWhereClause {
		if query, ok := call.Parameters["query"].(string); ok {
			userID := req.Metadata["user_id"]
			if userID != "" && strings.Contains(strings.ToUpper(query), "SELECT") {
				if !strings.Contains(strings.ToUpper(query), "WHERE") {
					call.Parameters["query"] = fmt.Sprintf("%s WHERE user_id = '%s'", query, userID)
					mutated = true
				}
			}
		}
	}

	if !mutated {
		return nil, nil
	}
	return &call, nil
}
