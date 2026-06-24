package validator

import (
	"context"
	"strings"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

func piiPolicy() policy.PIIBiometricPolicy {
	return policy.PIIBiometricPolicy{
		Enabled:              true,
		Patterns:             []string{`\b\d{3}-\d{2}-\d{4}\b`},
		BiometricKeySuffixes: []string{"_embedding", "_template", "voiceprint"},
		RedactReplacement:    "[REDACTED:PII]",
	}
}

func TestPIIBiometricValidator_Inbound(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string]any
		mc      *admission.MissionContext
		destURL string
		want    bool
		wantSev string
	}{
		{
			name:   "clean parameters",
			params: map[string]any{"customer_name": "Jane Doe"},
		},
		{
			name:    "biometric key, no mission context",
			params:  map[string]any{"face_embedding": []float64{0.1, 0.2}},
			want:    true,
			wantSev: "medium",
		},
		{
			name:    "SSN-shaped value, no mission context",
			params:  map[string]any{"note": "applicant ssn is 123-45-6789"},
			want:    true,
			wantSev: "medium",
		},
		{
			name:    "biometric key, destination out of declared scope",
			params:  map[string]any{"voiceprint": "abc123"},
			mc:      &admission.MissionContext{Scope: []string{"internal.example.com"}, OutOfScope: []string{"external.com"}},
			destURL: "https://external.com/upload",
			want:    true,
			wantSev: "critical",
		},
		{
			name:    "biometric key, destination within declared scope",
			params:  map[string]any{"voiceprint": "abc123"},
			mc:      &admission.MissionContext{Scope: []string{"internal.example.com"}, OutOfScope: []string{"external.com"}},
			destURL: "https://internal.example.com/api",
			want:    true,
			wantSev: "medium",
		},
		{
			name:    "biometric key, destination not in declared scope or out-of-scope list",
			params:  map[string]any{"voiceprint": "abc123"},
			mc:      &admission.MissionContext{Scope: []string{"internal.example.com"}},
			destURL: "https://unknown.com/x",
			want:    true,
			wantSev: "critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := NewPIIBiometricValidator(piiPolicy())
			if err != nil {
				t.Fatalf("NewPIIBiometricValidator: %v", err)
			}
			req := &admission.ToolCallAdmissionRequest{
				ToolCall: admission.ToolCall{
					Parameters:     tt.params,
					DestinationURL: tt.destURL,
				},
				MissionContext: tt.mc,
			}
			viol := v.Validate(context.Background(), req)
			if (viol != nil) != tt.want {
				t.Fatalf("want violation=%v got=%v", tt.want, viol)
			}
			if tt.want && viol.Severity != tt.wantSev {
				t.Errorf("want severity %q got %q", tt.wantSev, viol.Severity)
			}
		})
	}
}

func TestPIIBiometricValidator_Outbound(t *testing.T) {
	v, err := NewPIIBiometricValidator(piiPolicy())
	if err != nil {
		t.Fatalf("NewPIIBiometricValidator: %v", err)
	}

	t.Run("clean output", func(t *testing.T) {
		output := "here is your result"
		viol := v.Validate(context.Background(), &admission.ToolCallAdmissionRequest{ToolOutput: &output})
		if viol != nil {
			t.Fatalf("expected nil violation, got %+v", viol)
		}
	})

	t.Run("SSN-shaped output", func(t *testing.T) {
		output := "on file: 123-45-6789"
		viol := v.Validate(context.Background(), &admission.ToolCallAdmissionRequest{ToolOutput: &output})
		if viol == nil {
			t.Fatal("expected violation")
		}
		if viol.Severity != "low" {
			t.Errorf("want severity low, got %q", viol.Severity)
		}
	})
}

func TestPIIBiometricValidator_Disabled(t *testing.T) {
	p := piiPolicy()
	p.Enabled = false
	v, err := NewPIIBiometricValidator(p)
	if err != nil {
		t.Fatalf("NewPIIBiometricValidator: %v", err)
	}
	output := "123-45-6789"
	req := &admission.ToolCallAdmissionRequest{
		ToolCall: admission.ToolCall{Parameters: map[string]any{"face_embedding": "x"}},
		ToolOutput: &output,
	}
	if viol := v.Validate(context.Background(), req); viol != nil {
		t.Fatalf("expected nil violation when disabled, got %+v", viol)
	}
}

func TestPIIBiometricValidator_SanitizeOutput(t *testing.T) {
	v, err := NewPIIBiometricValidator(piiPolicy())
	if err != nil {
		t.Fatalf("NewPIIBiometricValidator: %v", err)
	}
	got := v.SanitizeOutput("ssn 123-45-6789 on file")
	if strings.Contains(got, "123-45-6789") {
		t.Errorf("expected SSN to be redacted, got %q", got)
	}
	if !strings.Contains(got, "[REDACTED:PII]") {
		t.Errorf("expected redaction marker, got %q", got)
	}
}

func TestPIIBiometricValidator_IsOutbound(t *testing.T) {
	v, err := NewPIIBiometricValidator(piiPolicy())
	if err != nil {
		t.Fatalf("NewPIIBiometricValidator: %v", err)
	}
	if !v.IsOutbound() {
		t.Fatal("expected IsOutbound() == true")
	}
}

func TestNewPIIBiometricValidator_InvalidPattern(t *testing.T) {
	p := piiPolicy()
	p.Patterns = []string{"["}
	if _, err := NewPIIBiometricValidator(p); err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}
