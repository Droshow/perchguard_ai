// Package pii provides deterministic, content-based detection and redaction of
// PII and biometric-template-shaped data. Unlike policies.audit.redactedFields
// (which matches by parameter *name* and is profile-specific), a Matcher scans
// actual content and parameter-key shapes, so it catches PII regardless of how
// a particular tool or profile names its fields.
package pii

import (
	"fmt"
	"regexp"
	"strings"
)

// defaultReplacement is used when no RedactReplacement is configured.
const defaultReplacement = "[REDACTED:PII]"

// Matcher detects and redacts PII/biometric content using a fixed set of
// regex patterns (for text values) and key-suffix matches (for parameter names).
type Matcher struct {
	patterns    []*regexp.Regexp
	keySuffixes []string
	replacement string
}

// NewMatcher compiles patterns and stores keySuffixes/replacement for later matching.
// keySuffixes are matched case-insensitively as substrings of parameter names
// (e.g. "_embedding" matches "face_embedding").
func NewMatcher(patterns, keySuffixes []string, replacement string) (*Matcher, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("pii: invalid pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}

	lowerSuffixes := make([]string, len(keySuffixes))
	for i, s := range keySuffixes {
		lowerSuffixes[i] = strings.ToLower(s)
	}

	if replacement == "" {
		replacement = defaultReplacement
	}

	return &Matcher{patterns: compiled, keySuffixes: lowerSuffixes, replacement: replacement}, nil
}

// MatchText reports whether text contains content matching any configured
// pattern, returning the matched substring as label.
func (m *Matcher) MatchText(text string) (label string, ok bool) {
	for _, re := range m.patterns {
		if hit := re.FindString(text); hit != "" {
			return hit, true
		}
	}
	return "", false
}

// MatchKey reports whether a parameter key looks like a biometric/PII field —
// a case-insensitive substring match against the configured key suffixes
// (e.g. "face_embedding" matches suffix "_embedding").
func (m *Matcher) MatchKey(key string) bool {
	lower := strings.ToLower(key)
	for _, suffix := range m.keySuffixes {
		if strings.Contains(lower, suffix) {
			return true
		}
	}
	return false
}

// Redact replaces every pattern match in text with the configured replacement.
func (m *Matcher) Redact(text string) string {
	for _, re := range m.patterns {
		text = re.ReplaceAllString(text, m.replacement)
	}
	return text
}
