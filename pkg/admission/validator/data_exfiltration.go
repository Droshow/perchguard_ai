package validator

import (
	"context"
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// DataExfiltrationValidator blocks outbound tool calls to unknown/untrusted destinations.
//
// This is the agentic equivalent of:
//   network_security/check_egress.go  - enforces allowed egress CIDRs for pods
//
// Attack scenario: Indirect prompt injection causes the agent to believe it should
// POST sensitive data to attacker.com. PerchGuard's outbound admission policy
// blocks this regardless of how "convinced" the agent is.
//
// We inspect:
//   - The DestinationURL field of the ToolCall
//   - URL parameters in bash/curl commands
//   - Webhook URLs in tool parameters
type DataExfiltrationValidator struct {
	policy policy.DataExfiltrationPolicy
}

func NewDataExfiltrationValidator(p policy.DataExfiltrationPolicy) *DataExfiltrationValidator {
	return &DataExfiltrationValidator{policy: p}
}

func (v *DataExfiltrationValidator) Name() string { return "data_exfiltration" }

func (v *DataExfiltrationValidator) Validate(ctx context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	if !v.policy.Enabled {
		return nil
	}

	// Collect all URLs this tool call might contact
	urls := v.extractURLs(req)
	if len(urls) == 0 {
		return nil
	}

	for _, url := range urls {
		if violation := v.checkURL(url, req.AgentRole); violation != nil {
			return violation
		}
	}

	return nil
}

func (v *DataExfiltrationValidator) extractURLs(req *admission.ToolCallAdmissionRequest) []string {
	var urls []string

	// Direct DestinationURL field
	if req.ToolCall.DestinationURL != "" {
		urls = append(urls, req.ToolCall.DestinationURL)
	}

	// Extract from parameters (covers curl commands, webhook calls, etc.)
	for _, val := range req.ToolCall.Parameters {
		if s, ok := val.(string); ok {
			extracted := extractURLsFromString(s)
			urls = append(urls, extracted...)
		}
	}

	return urls
}

func (v *DataExfiltrationValidator) checkURL(url, role string) *admission.PolicyViolation {
	// Check explicit block list first
	for _, blocked := range v.policy.BlockedDestinations {
		if matchesHostPattern(url, blocked) {
			return &admission.PolicyViolation{
				Layer:    "validation",
				Policy:   "dataExfiltration.blockList",
				Detail:   "Destination '" + url + "' is explicitly blocked (matches '" + blocked + "')",
				Severity: "critical",
			}
		}
	}

	// Check if it's in the allow list
	for _, allowed := range v.policy.AllowedDestinations {
		if matchesHostPattern(url, allowed) {
			return nil // explicitly allowed
		}
	}

	// Unknown destination
	if v.policy.BlockUnknownDestinations {
		return &admission.PolicyViolation{
			Layer:    "validation",
			Policy:   "dataExfiltration.unknownDestination",
			Detail:   "Destination '" + url + "' is not in the allow list and blockUnknownDestinations=true",
			Severity: "high",
		}
	}

	return nil
}

// matchesHostPattern checks if a URL matches a host pattern.
// Supports wildcards: "*.ngrok.io" matches "abc123.ngrok.io"
func matchesHostPattern(url, pattern string) bool {
	// Normalize: strip scheme
	url = stripScheme(url)
	pattern = stripScheme(pattern)

	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".ngrok.io"
		return strings.Contains(url, suffix)
	}
	return strings.HasPrefix(url, pattern) || strings.Contains(url, pattern)
}

func stripScheme(url string) string {
	for _, scheme := range []string{"https://", "http://", "ftp://"} {
		url = strings.TrimPrefix(url, scheme)
	}
	return url
}

// extractURLsFromString finds URL-like strings and email domains in a parameter value.
// Email domains (user@evil.io) are returned as bare hostnames so the same
// block/allow list matching applies — catching send_notification exfil attempts
// that use email addresses rather than explicit http:// URLs.
func extractURLsFromString(s string) []string {
	var urls []string
	words := strings.Fields(s)
	for _, word := range words {
		word = strings.Trim(word, "\"'`,;")
		if strings.HasPrefix(word, "http://") || strings.HasPrefix(word, "https://") {
			urls = append(urls, word)
			continue
		}
		// email-shaped: something@domain.tld — extract the domain for destination checking
		if idx := strings.Index(word, "@"); idx > 0 && idx < len(word)-1 {
			domain := word[idx+1:]
			if strings.Contains(domain, ".") {
				urls = append(urls, domain)
			}
		}
	}
	return urls
}
