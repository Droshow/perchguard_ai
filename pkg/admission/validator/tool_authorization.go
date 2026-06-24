package validator

import (
	"context"
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// ToolAuthorizationValidator enforces role-based tool allow/deny lists.
//
// Analogy to EKS-BankingKube:
//   rbac_checks/check_permission_levels.go  - restricts what verbs/resources a role can use
//   api_restrictions/check_api_access.go    - blocks access to restricted API paths
//
// Here we ask the same question at the agentic layer:
//   "Is this agent ROLE allowed to call THIS TOOL with THESE PARAMETERS?"
//
// Example policy (from policies.yaml):
//   read_only_agent: allowed=[read_file, list_directory], denied=[bash, write_file]
//   developer_agent: allowed=[bash, write_file], denied=[bash:rm -rf, database:drop_*]
type ToolAuthorizationValidator struct {
	policy policy.ToolAuthorizationPolicy
}

func NewToolAuthorizationValidator(p policy.ToolAuthorizationPolicy) *ToolAuthorizationValidator {
	return &ToolAuthorizationValidator{policy: p}
}

func (v *ToolAuthorizationValidator) Name() string { return "tool_authorization" }

func (v *ToolAuthorizationValidator) Validate(ctx context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	if !v.policy.Enabled {
		return nil
	}

	// Find the matching role for this agent
	var matchedRole *policy.AgentRole
	for i := range v.policy.AgentRoles {
		if v.policy.AgentRoles[i].Role == req.AgentRole {
			matchedRole = &v.policy.AgentRoles[i]
			break
		}
	}

	if matchedRole == nil {
		// Unknown role - default deny (fail-closed, mirrors K8s RBAC default)
		return &admission.PolicyViolation{
			Layer:    "validation",
			Policy:   "toolAuthorization.unknownRole",
			Detail:   "Agent role '" + req.AgentRole + "' is not defined in policy - denying by default",
			Severity: "high",
		}
	}

	toolCallSig := buildToolSignature(req)

	// Check denied list first (explicit deny wins, like K8s RBAC)
	for _, denied := range matchedRole.DeniedTools {
		if matchesToolPattern(toolCallSig, denied) {
			return &admission.PolicyViolation{
				Layer:    "validation",
				Policy:   "toolAuthorization.denyList",
				Detail:   "Tool '" + toolCallSig + "' is explicitly denied for role '" + req.AgentRole + "'",
				Severity: "critical",
			}
		}
	}

	// Check allowed list (if non-empty, it's an allowlist - deny everything else)
	if len(matchedRole.AllowedTools) > 0 {
		for _, allowed := range matchedRole.AllowedTools {
			if matchesToolPattern(toolCallSig, allowed) {
				return nil // explicitly allowed
			}
		}
		return &admission.PolicyViolation{
			Layer:    "validation",
			Policy:   "toolAuthorization.allowList",
			Detail:   "Tool '" + toolCallSig + "' is not in the allow list for role '" + req.AgentRole + "'",
			Severity: "high",
		}
	}

	return nil // no allowlist defined, passed deny check - allow
}

// buildToolSignature creates a matchable string from the tool call.
// For "bash" tool with command "rm -rf /tmp", returns "bash:rm -rf /tmp"
// This allows policies like "bash:rm*" to match any bash rm command.
func buildToolSignature(req *admission.ToolCallAdmissionRequest) string {
	// Normalize to lowercase: Claude Code sends "Bash", "Read", "Write", etc.
	// Policy lists use lowercase. Normalize here so all downstream matching is consistent.
	name := strings.ToLower(req.ToolCall.Name)
	// For bash tool, include the command for fine-grained matching
	if name == "bash" {
		if cmd, ok := req.ToolCall.Parameters["command"].(string); ok {
			return "bash:" + cmd
		}
	}
	return name
}

// matchesToolPattern checks if a tool signature matches a policy pattern.
//
// Signatures are built as "toolname" or "toolname:argument" (e.g. "bash:rm -rf /tmp").
// Patterns can be:
//   - Exact:          "bash:git status"   matches only that exact call
//   - Plain name:     "bash"              matches "bash" AND "bash:anything" (any bash call)
//   - Wildcard suffix:"bash:rm*"          matches "bash:rm -rf /tmp"
//   - Wildcard glob:  "bash:*--force*"    matches any bash call containing --force
func matchesToolPattern(signature, pattern string) bool {
	if pattern == signature {
		return true
	}
	// Plain tool name (no ":" or "*"): matches the bare name OR any "name:..." call.
	// "bash" matches "bash" and "bash:ls /" but not "bash2".
	if !strings.Contains(pattern, ":") && !strings.Contains(pattern, "*") {
		return strings.HasPrefix(strings.ToLower(signature), strings.ToLower(pattern)+":")
	}
	// Wildcard suffix: "bash:rm*"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if strings.HasPrefix(strings.ToLower(signature), strings.ToLower(prefix)) {
			return true
		}
	}
	// Wildcard content: "bash:*--force*"
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		pos := 0
		lsig := strings.ToLower(signature)
		for _, part := range parts {
			if part == "" {
				continue
			}
			idx := strings.Index(lsig[pos:], strings.ToLower(part))
			if idx == -1 {
				return false
			}
			pos += idx + len(part)
		}
		return true
	}
	return false
}
