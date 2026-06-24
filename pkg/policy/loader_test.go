package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policies.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		path    string // override yaml with a literal path (for missing-file case)
		wantErr bool
		check   func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid full YAML loads without error",
			yaml: `
policies:
  promptInjection:
    enabled: true
    suspiciousPatterns:
      - "ignore previous instructions"
    scanFields:
      - user_intent
    action: DENY
  toolAuthorization:
    enabled: true
    agentRoles:
      - role: developer_agent
        allowedTools:
          - read_file
    action: DENY
  sessionBudget:
    enabled: true
    limits:
      maxTokensPerSession: 100000
      maxCostPerSessionUSD: 5.0
      maxToolCallsPerSession: 200
  depthLimiter:
    enabled: true
    maxAgentNestingDepth: 5
  outputValidation:
    enabled: true
    maxOutputSizeBytes: 1048576
    action: SANITIZE
    sanitizeMode: redact
    redactReplacement: "[REDACTED]"
  piiBiometric:
    enabled: true
    patterns:
      - "\\b\\d{3}-\\d{2}-\\d{4}\\b"
    biometricKeySuffixes:
      - "_embedding"
      - "_template"
      - "voiceprint"
    redactReplacement: "[REDACTED:PII]"
  humanReview:
    enabled: false
    timeoutSeconds: 60
  audit:
    enabled: true
`,
			check: func(t *testing.T, cfg *Config) {
				if !cfg.Policies.PromptInjection.Enabled {
					t.Error("want promptInjection.enabled = true")
				}
				if cfg.Policies.DepthLimiter.MaxAgentNestingDepth != 5 {
					t.Errorf("want maxAgentNestingDepth=5, got %d", cfg.Policies.DepthLimiter.MaxAgentNestingDepth)
				}
				if cfg.Policies.OutputValidation.SanitizeMode != "redact" {
					t.Errorf("want sanitizeMode=redact, got %q", cfg.Policies.OutputValidation.SanitizeMode)
				}
				if !cfg.Policies.PIIBiometric.Enabled {
					t.Error("want piiBiometric.enabled = true")
				}
				if len(cfg.Policies.PIIBiometric.BiometricKeySuffixes) != 3 {
					t.Errorf("want 3 biometricKeySuffixes, got %d", len(cfg.Policies.PIIBiometric.BiometricKeySuffixes))
				}
			},
		},
		{
			name: "minimal YAML with missing optional fields does not panic",
			yaml: "policies: {}\n",
			check: func(t *testing.T, cfg *Config) {
				// zero values are fine — no panic is the assertion
				_ = cfg.Policies.PromptInjection.Enabled
				_ = cfg.Policies.SessionBudget.Limits.MaxToolCallsPerSession
			},
		},
		{
			name:    "non-existent file returns error",
			path:    "/no/such/file.yaml",
			wantErr: true,
		},
		{
			name:    "malformed YAML returns error",
			yaml:    "policies:\n  invalid: [\nunclosed bracket",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = writeTemp(t, tc.yaml)
			}
			cfg, err := Load(path)
			if tc.wantErr {
				if err == nil {
					t.Error("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg == nil {
				t.Fatal("want non-nil config, got nil")
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}
