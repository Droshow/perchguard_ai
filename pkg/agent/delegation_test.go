package agent

import (
	"sync"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// ── DelegationStore ───────────────────────────────────────────────────────────

func TestDelegationStore_RecordAndParent(t *testing.T) {
	ds := NewDelegationStore()
	ds.Record("child-1", "parent-1")

	p, ok := ds.Parent("child-1")
	if !ok || p != "parent-1" {
		t.Errorf("want parent-1, got %q ok=%v", p, ok)
	}
	_, ok = ds.Parent("unknown")
	if ok {
		t.Error("expected miss for unknown session")
	}
}

func TestDelegationStore_Depth(t *testing.T) {
	ds := NewDelegationStore()
	// root → child → grandchild
	ds.Record("child", "root")
	ds.Record("grandchild", "child")

	if d := ds.Depth("root"); d != 0 {
		t.Errorf("root depth: want 0, got %d", d)
	}
	if d := ds.Depth("child"); d != 1 {
		t.Errorf("child depth: want 1, got %d", d)
	}
	if d := ds.Depth("grandchild"); d != 2 {
		t.Errorf("grandchild depth: want 2, got %d", d)
	}
}

func TestDelegationStore_Evict(t *testing.T) {
	ds := NewDelegationStore()
	ds.Record("child", "parent")
	ds.Evict("child")

	_, ok := ds.Parent("child")
	if ok {
		t.Error("expected child to be gone after Evict")
	}
}

func TestDelegationStore_ConcurrentAccess(t *testing.T) {
	ds := NewDelegationStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			child := "child-" + string(rune('A'+i%10))
			ds.Record(child, "root")
			ds.Parent(child)
			ds.Depth(child)
		}(i)
	}
	wg.Wait()
}

// ── ValidateScope ─────────────────────────────────────────────────────────────

func makeManifest(role string) *manifest.AgentManifest {
	return &manifest.AgentManifest{
		Metadata:      manifest.Metadata{ID: "agent", Version: "1"},
		Mission:       manifest.Mission{Summary: "test"},
		Authorization: manifest.Authorization{Role: role},
	}
}

func policyWithRoles(roles ...policy.AgentRole) *policy.Config {
	return &policy.Config{
		Policies: policy.Policies{
			ToolAuthorization: policy.ToolAuthorizationPolicy{
				Enabled:    true,
				AgentRoles: roles,
			},
		},
	}
}

func TestValidateScope_ToolAuthDisabled(t *testing.T) {
	cfg := &policy.Config{
		Policies: policy.Policies{
			ToolAuthorization: policy.ToolAuthorizationPolicy{Enabled: false},
		},
	}
	// Any combination passes when tool auth is off.
	if err := ValidateScope(makeManifest("any-child"), makeManifest("any-parent"), cfg); err != nil {
		t.Errorf("want nil when tool auth disabled, got %v", err)
	}
}

func TestValidateScope_ParentUnrestricted(t *testing.T) {
	// Parent role not in policy → treated as unrestricted → child always passes.
	cfg := policyWithRoles(policy.AgentRole{
		Role:         "read_only",
		AllowedTools: []string{"read_file"},
	})
	child := makeManifest("read_only")
	parent := makeManifest("unknown_role") // not in policy → no restriction
	if err := ValidateScope(child, parent, cfg); err != nil {
		t.Errorf("want nil for unrestricted parent, got %v", err)
	}
}

func TestValidateScope_ValidSubset(t *testing.T) {
	cfg := policyWithRoles(
		policy.AgentRole{Role: "admin", AllowedTools: []string{"read_file", "write_file", "exec"}},
		policy.AgentRole{Role: "reader", AllowedTools: []string{"read_file"}},
	)
	child := makeManifest("reader") // {read_file} ⊆ {read_file, write_file, exec}
	parent := makeManifest("admin")
	if err := ValidateScope(child, parent, cfg); err != nil {
		t.Errorf("want nil for valid subset, got %v", err)
	}
}

func TestValidateScope_InvalidSuperset(t *testing.T) {
	cfg := policyWithRoles(
		policy.AgentRole{Role: "reader", AllowedTools: []string{"read_file"}},
		policy.AgentRole{Role: "writer", AllowedTools: []string{"read_file", "write_file"}},
	)
	child := makeManifest("writer") // write_file not in parent's {read_file}
	parent := makeManifest("reader")
	if err := ValidateScope(child, parent, cfg); err == nil {
		t.Error("want error when child has tools beyond parent scope, got nil")
	}
}

func TestValidateScope_SameRole(t *testing.T) {
	cfg := policyWithRoles(
		policy.AgentRole{Role: "analyst", AllowedTools: []string{"read_file", "run_sql"}},
	)
	// Child with same role as parent is always valid.
	if err := ValidateScope(makeManifest("analyst"), makeManifest("analyst"), cfg); err != nil {
		t.Errorf("want nil for same role delegation, got %v", err)
	}
}

func TestValidateScope_UnknownChildRole(t *testing.T) {
	cfg := policyWithRoles(
		policy.AgentRole{Role: "admin", AllowedTools: []string{"read_file", "exec"}},
	)
	child := makeManifest("ghost") // not in policy → empty tool set → ⊆ anything → passes
	parent := makeManifest("admin")
	if err := ValidateScope(child, parent, cfg); err != nil {
		t.Errorf("want nil for unknown child role (empty set is subset of anything), got %v", err)
	}
}
