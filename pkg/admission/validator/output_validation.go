package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// OutputValidator scans tool outputs for injected instructions before they
// are fed back to the agent. Implements OutboundValidator — only runs on
// the return path (when req.ToolOutput != nil).
//
// Returns medium-severity violations → interceptor maps these to MUTATE
// with SanitizedOutput populated. High-severity (size exceeded) → DENY.
type OutputValidator struct {
	policy policy.OutputValidationPolicy
}

func NewOutputValidator(p policy.OutputValidationPolicy) *OutputValidator {
	return &OutputValidator{policy: p}
}

func (v *OutputValidator) Name() string      { return "output_validation" }
func (v *OutputValidator) IsOutbound() bool  { return true }

func (v *OutputValidator) Validate(ctx context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	if !v.policy.Enabled || req.ToolOutput == nil {
		return nil
	}

	output := *req.ToolOutput

	if v.policy.MaxOutputSizeBytes > 0 && len(output) > v.policy.MaxOutputSizeBytes {
		return &admission.PolicyViolation{
			Layer:    "outbound",
			Policy:   "outputValidation.maxSize",
			Detail:   fmt.Sprintf("tool output size %d exceeds limit %d", len(output), v.policy.MaxOutputSizeBytes),
			Severity: "high",
		}
	}

	lower := strings.ToLower(output)
	for _, pattern := range v.policy.SuspiciousOutputPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return &admission.PolicyViolation{
				Layer:    "outbound",
				Policy:   "outputValidation.suspiciousPattern",
				Detail:   fmt.Sprintf("suspicious pattern in tool output: %q", pattern),
				Severity: "medium",
			}
		}
	}
	return nil
}

// SanitizeOutput strips or redacts all suspicious patterns from the output.
// Implements admission.Sanitizer — called by the interceptor when decision is MUTATE.
//
// For the "redact" mode (default): when a pattern is found, the entire line
// containing the pattern is replaced with the redaction marker. This ensures
// the payload text that follows a marker like [[HIDDEN INSTRUCTION]] is also
// removed, not just the marker itself.
func (v *OutputValidator) SanitizeOutput(output string) string {
	replacement := v.policy.RedactReplacement
	if replacement == "" {
		replacement = "[REDACTED]"
	}
	for _, pattern := range v.policy.SuspiciousOutputPatterns {
		lowerPattern := strings.ToLower(pattern)
		switch v.policy.SanitizeMode {
		case "strip":
			// Strip only the matched token, case-insensitive.
			output = replaceAllFold(output, lowerPattern, "")
		case "block":
			// Treat as hard stop — caller should DENY instead; return marker.
			return "[OUTPUT BLOCKED]"
		default: // "redact"
			// Replace the entire line containing the pattern so the payload
			// text that follows the marker is also removed.
			output = redactLinesContaining(output, lowerPattern, replacement)
		}
	}
	return output
}

// redactLinesContaining replaces every line that contains the given substring
// (case-insensitive) with the replacement marker.
func redactLinesContaining(text, lowerPattern, replacement string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), lowerPattern) {
			lines[i] = replacement
		}
	}
	return strings.Join(lines, "\n")
}

// replaceAllFold replaces all occurrences of lowerPattern (already lowercased)
// in text, preserving the original casing of the surrounding text.
func replaceAllFold(text, lowerPattern, replacement string) string {
	lowerText := strings.ToLower(text)
	var result strings.Builder
	offset := 0
	for {
		idx := strings.Index(lowerText[offset:], lowerPattern)
		if idx < 0 {
			result.WriteString(text[offset:])
			break
		}
		result.WriteString(text[offset : offset+idx])
		result.WriteString(replacement)
		offset += idx + len(lowerPattern)
	}
	return result.String()
}
