package validator

import (
	"context"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

func TestToolAuthorizationCaseInsensitive(t *testing.T) {
	pol := policy.ToolAuthorizationPolicy{
		Enabled: true,
		AgentRoles: []policy.AgentRole{
			{
				Role:         "developer_agent",
				AllowedTools: []string{"bash", "read_file"},
				DeniedTools:  []string{"bash:rm -rf"},
			},
		},
	}
	v := NewToolAuthorizationValidator(pol)

	tests := []struct {
		toolName string
		wantDeny bool
		note     string
	}{
		{"bash", false, "lowercase matches allow list"},
		{"Bash", false, "Claude Code capitalised — must match allow list"},
		{"BASH", false, "all-caps normalises"},
		{"read_file", false, "snake_case tool allowed"},
		{"Read_File", false, "capitalised snake_case matches allow list"},
		{"write_file", true, "not in allow list → deny"},
		{"Write_File", true, "capitalised Write_File not in allow list → deny"},
	}

	for _, tt := range tests {
		t.Run(tt.note, func(t *testing.T) {
			req := &admission.ToolCallAdmissionRequest{
				AgentRole: "developer_agent",
				ToolCall:  admission.ToolCall{Name: tt.toolName, Parameters: map[string]any{}},
			}
			viol := v.Validate(context.Background(), req)
			gotDeny := viol != nil
			if gotDeny != tt.wantDeny {
				t.Errorf("tool=%q: got deny=%v want deny=%v (violation=%v)", tt.toolName, gotDeny, tt.wantDeny, viol)
			}
		})
	}
}

func TestToolAuthorizationDenyListCaseInsensitive(t *testing.T) {
	pol := policy.ToolAuthorizationPolicy{
		Enabled: true,
		AgentRoles: []policy.AgentRole{
			{
				Role:        "read_only_agent",
				DeniedTools: []string{"bash", "write_file"},
			},
		},
	}
	v := NewToolAuthorizationValidator(pol)

	tests := []struct {
		toolName string
		wantDeny bool
	}{
		{"bash", true},
		{"Bash", true}, // Claude Code capitalisation
		{"BASH", true},
		{"read_file", false},
		{"Read", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			req := &admission.ToolCallAdmissionRequest{
				AgentRole: "read_only_agent",
				ToolCall:  admission.ToolCall{Name: tt.toolName, Parameters: map[string]any{}},
			}
			viol := v.Validate(context.Background(), req)
			gotDeny := viol != nil
			if gotDeny != tt.wantDeny {
				t.Errorf("tool=%q: got deny=%v want deny=%v", tt.toolName, gotDeny, tt.wantDeny)
			}
		})
	}
}

func TestBuildToolSignatureNormalisesCase(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string]any
		wantSig string
	}{
		{"bash", map[string]any{"command": "ls"}, "bash:ls"},
		{"Bash", map[string]any{"command": "ls"}, "bash:ls"},
		{"BASH", map[string]any{"command": "ls"}, "bash:ls"},
		{"Read", map[string]any{}, "read"},
		{"Write", map[string]any{}, "write"},
		{"read_file", map[string]any{}, "read_file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &admission.ToolCallAdmissionRequest{
				ToolCall: admission.ToolCall{Name: tt.name, Parameters: tt.params},
			}
			got := buildToolSignature(req)
			if got != tt.wantSig {
				t.Errorf("buildToolSignature(%q) = %q, want %q", tt.name, got, tt.wantSig)
			}
		})
	}
}
