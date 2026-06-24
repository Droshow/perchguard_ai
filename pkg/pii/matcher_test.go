package pii

import "testing"

const ssnPattern = `\b\d{3}-\d{2}-\d{4}\b`

func TestNewMatcher_InvalidPattern(t *testing.T) {
	if _, err := NewMatcher([]string{"["}, nil, ""); err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestMatchText(t *testing.T) {
	m, err := NewMatcher([]string{ssnPattern}, nil, "")
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	tests := []struct {
		name string
		text string
		want bool
	}{
		{"ssn present", "applicant ssn is 123-45-6789", true},
		{"no ssn", "applicant name is Jane Doe", false},
		{"malformed ssn", "ref number 123-456-789", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := m.MatchText(tt.text)
			if ok != tt.want {
				t.Errorf("MatchText(%q) ok=%v, want %v", tt.text, ok, tt.want)
			}
		})
	}
}

func TestMatchKey(t *testing.T) {
	m, err := NewMatcher(nil, []string{"_embedding", "_template", "voiceprint"}, "")
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	tests := []struct {
		key  string
		want bool
	}{
		{"face_embedding", true},
		{"iris_template", true},
		{"voiceprint", true},
		{"VoicePrint", true}, // case-insensitive
		{"customer_name", false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := m.MatchKey(tt.key); got != tt.want {
				t.Errorf("MatchKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	m, err := NewMatcher([]string{ssnPattern}, nil, "")
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	got := m.Redact("ssn 123-45-6789 on file")
	want := "ssn [REDACTED:PII] on file"
	if got != want {
		t.Errorf("Redact() = %q, want %q", got, want)
	}
}

func TestRedact_CustomReplacement(t *testing.T) {
	m, err := NewMatcher([]string{ssnPattern}, nil, "[SCRUBBED]")
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	got := m.Redact("ssn 123-45-6789")
	if got != "ssn [SCRUBBED]" {
		t.Errorf("Redact() = %q, want %q", got, "ssn [SCRUBBED]")
	}
}
