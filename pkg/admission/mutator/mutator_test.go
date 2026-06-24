package mutator

import (
	"context"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

func TestParameterSanitizer(t *testing.T) {
	tests := []struct {
		name        string
		policy      policy.ParameterSanitizationPolicy
		params      map[string]any
		wantNilResult bool
		wantCommand string // non-empty: check params["command"]
	}{
		{
			name: "clean params pass through unchanged",
			policy: policy.ParameterSanitizationPolicy{
				Enabled: true,
				Rules: []policy.SanitizationRule{
					{ContainsAny: []string{"rm -rf"}, Inject: "--dry-run"},
				},
			},
			params:        map[string]any{"command": "ls /tmp"},
			wantNilResult: true, // no mutation → nil return
		},
		{
			name: "strip dangerous flag",
			policy: policy.ParameterSanitizationPolicy{
				Enabled: true,
				Rules: []policy.SanitizationRule{
					{StripFlags: []string{"--force"}},
				},
			},
			params:        map[string]any{"command": "git push --force origin main"},
			wantNilResult: false,
			wantCommand:   "git push  origin main", // --force replaced by ""
		},
		{
			name: "disabled policy is no-op",
			policy: policy.ParameterSanitizationPolicy{
				Enabled: false,
				Rules: []policy.SanitizationRule{
					{StripFlags: []string{"--force"}},
				},
			},
			params:        map[string]any{"command": "git push --force"},
			wantNilResult: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m := NewParameterSanitizer(tc.policy)
			req := &admission.ToolCallAdmissionRequest{
				ToolCall: admission.ToolCall{
					Name:       "bash",
					Parameters: tc.params,
				},
			}
			result, err := m.Mutate(context.Background(), req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNilResult && result != nil {
				t.Errorf("want nil result, got %+v", result)
			}
			if !tc.wantNilResult && result == nil {
				t.Fatal("want non-nil result, got nil")
			}
			if tc.wantCommand != "" {
				got, _ := result.Parameters["command"].(string)
				if got != tc.wantCommand {
					t.Errorf("want command %q got %q", tc.wantCommand, got)
				}
			}
		})
	}
}

func TestLeastPrivilegeMutator(t *testing.T) {
	tests := []struct {
		name        string
		policy      policy.LeastPrivilegePolicy
		params      map[string]any
		metadata    map[string]string
		wantErr     bool
		wantNilResult bool
		wantQueryContains string
	}{
		{
			name: "path traversal blocked returns error",
			policy: policy.LeastPrivilegePolicy{
				Enabled:        true,
				FileOperations: policy.FilePrivPolicy{BlockPathTraversal: true},
			},
			params:  map[string]any{"path": "../../etc/passwd"},
			wantErr: true,
		},
		{
			name: "absolute path outside allowed base blocked",
			policy: policy.LeastPrivilegePolicy{
				Enabled: true,
				FileOperations: policy.FilePrivPolicy{
					AllowedBasePaths:   []string{"/workspace"},
					BlockAbsolutePaths: true,
				},
			},
			params:  map[string]any{"path": "/etc/passwd"},
			wantErr: true,
		},
		{
			name: "SQL WHERE clause injected",
			policy: policy.LeastPrivilegePolicy{
				Enabled: true,
				SQLInjection: policy.SQLPrivPolicy{AutoAppendWhereClause: true},
			},
			params:   map[string]any{"query": "SELECT * FROM accounts"},
			metadata: map[string]string{"user_id": "u123"},
			wantNilResult: false,
			wantQueryContains: "WHERE user_id = 'u123'",
		},
		{
			name: "disabled policy is no-op",
			policy: policy.LeastPrivilegePolicy{
				Enabled:        false,
				FileOperations: policy.FilePrivPolicy{BlockPathTraversal: true},
			},
			params:        map[string]any{"path": "../../etc/passwd"},
			wantNilResult: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m := NewLeastPrivilegeMutator(tc.policy)
			req := &admission.ToolCallAdmissionRequest{
				ToolCall: admission.ToolCall{
					Name:       "tool",
					Parameters: tc.params,
				},
				Metadata: tc.metadata,
			}
			result, err := m.Mutate(context.Background(), req)
			if tc.wantErr {
				if err == nil {
					t.Error("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNilResult && result != nil {
				t.Errorf("want nil result, got %+v", result)
			}
			if !tc.wantNilResult && result == nil {
				t.Fatal("want non-nil result, got nil")
			}
			if tc.wantQueryContains != "" {
				q, _ := result.Parameters["query"].(string)
				if len(q) == 0 || len(tc.wantQueryContains) == 0 {
					t.Fatal("empty query")
				}
				found := false
				for i := 0; i <= len(q)-len(tc.wantQueryContains); i++ {
					if q[i:i+len(tc.wantQueryContains)] == tc.wantQueryContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("want query to contain %q, got %q", tc.wantQueryContains, q)
				}
			}
		})
	}
}
