package validator

import (
	"context"
	"fmt"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/pii"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// PIIBiometricValidator detects PII and biometric-template-shaped content in tool
// call parameters and tool outputs, independent of policies.audit.redactedFields
// (which matches by parameter *name* and is profile-specific). See pkg/pii.
//
// Inbound: a biometric-shaped parameter key (e.g. "face_embedding") or PII-shaped
// parameter value (e.g. an SSN) is "critical" (-> DENY) when the call's destination
// falls outside the agent's declared mission scope, and "medium" (-> HUMAN_REVIEW)
// otherwise -- visibility without blocking legitimate in-scope handling.
//
// Outbound: matching content in a tool's output is "low" (-> MUTATE), and
// SanitizeOutput redacts it before the output is returned to the agent.
type PIIBiometricValidator struct {
	policy  policy.PIIBiometricPolicy
	matcher *pii.Matcher
}

// NewPIIBiometricValidator builds the validator's pii.Matcher from policy config.
// Returns an error if any configured pattern is an invalid regex.
func NewPIIBiometricValidator(p policy.PIIBiometricPolicy) (*PIIBiometricValidator, error) {
	m, err := pii.NewMatcher(p.Patterns, p.BiometricKeySuffixes, p.RedactReplacement)
	if err != nil {
		return nil, err
	}
	return &PIIBiometricValidator{policy: p, matcher: m}, nil
}

func (v *PIIBiometricValidator) Name() string     { return "pii_biometric" }
func (v *PIIBiometricValidator) IsOutbound() bool { return true }

func (v *PIIBiometricValidator) Validate(ctx context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	if !v.policy.Enabled {
		return nil
	}

	if req.ToolOutput != nil {
		if _, ok := v.matcher.MatchText(*req.ToolOutput); ok {
			return &admission.PolicyViolation{
				Layer:    "outbound",
				Policy:   "piiBiometric.outputScan",
				Detail:   "PII-shaped content detected in tool output",
				Severity: "low",
			}
		}
		return nil
	}

	for key, val := range req.ToolCall.Parameters {
		if v.matcher.MatchKey(key) {
			return &admission.PolicyViolation{
				Layer:    "validation",
				Policy:   "piiBiometric.biometricKey",
				Detail:   fmt.Sprintf("biometric-shaped parameter %q", key),
				Severity: severityForScope(req),
			}
		}
		if s, ok := val.(string); ok {
			if _, ok := v.matcher.MatchText(s); ok {
				return &admission.PolicyViolation{
					Layer:    "validation",
					Policy:   "piiBiometric.contentScan",
					Detail:   fmt.Sprintf("PII-shaped content in parameter %q", key),
					Severity: severityForScope(req),
				}
			}
		}
	}
	return nil
}

// SanitizeOutput redacts all matching PII/biometric content from a tool output.
// Implements admission.Sanitizer — called by the interceptor when the outbound
// decision is MUTATE.
func (v *PIIBiometricValidator) SanitizeOutput(output string) string {
	return v.matcher.Redact(output)
}

// severityForScope returns "critical" when the call's destination falls outside
// the agent's declared mission scope, "medium" otherwise. Mirrors
// DataExfiltrationValidator's boundary check (matchesHostPattern), but against
// MissionContext.Scope/OutOfScope rather than policy-level allow/block lists.
func severityForScope(req *admission.ToolCallAdmissionRequest) string {
	mc := req.MissionContext
	dest := req.ToolCall.DestinationURL
	if mc == nil || dest == "" {
		return "medium"
	}
	for _, oos := range mc.OutOfScope {
		if matchesHostPattern(dest, oos) {
			return "critical"
		}
	}
	for _, s := range mc.Scope {
		if matchesHostPattern(dest, s) {
			return "medium"
		}
	}
	if len(mc.Scope) > 0 {
		return "critical"
	}
	return "medium"
}
