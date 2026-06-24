package agent

import (
	"fmt"
	"sync"

	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
)

// DelegationStore tracks the parent→child session relationships across the fleet.
// It is the authoritative record of which sessions are governed sub-agents and
// how deep each delegation chain runs.
type DelegationStore struct {
	mu      sync.RWMutex
	parents map[string]string // childSessionID → parentSessionID
}

func NewDelegationStore() *DelegationStore {
	return &DelegationStore{parents: make(map[string]string)}
}

// Record registers childSessionID as a sub-agent of parentSessionID.
func (ds *DelegationStore) Record(childSessionID, parentSessionID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.parents[childSessionID] = parentSessionID
}

// Parent returns the parent session ID for a child, or false if none.
func (ds *DelegationStore) Parent(childSessionID string) (string, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	p, ok := ds.parents[childSessionID]
	return p, ok
}

// Depth returns how many hops up the delegation chain sessionID sits.
// A root session (no parent) returns 0. A direct sub-agent returns 1.
// Capped at 100 to guard against any accidental cycle.
func (ds *DelegationStore) Depth(sessionID string) int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	depth := 0
	current := sessionID
	for range [100]struct{}{} {
		parent, ok := ds.parents[current]
		if !ok {
			break
		}
		depth++
		current = parent
	}
	return depth
}

// Evict removes a session from the delegation store on session close.
func (ds *DelegationStore) Evict(sessionID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.parents, sessionID)
}

// ValidateScope asserts that the child agent's tool scope is a subset of the
// parent agent's tool scope according to the active policy.
//
// When tool authorization is disabled in policy, any delegation is valid.
// When the parent's role has no restricted tool list (unknown role or empty
// AllowedTools), the parent is treated as unrestricted and any child scope passes.
// When the parent is restricted, every tool in the child's AllowedTools must
// appear in the parent's AllowedTools — otherwise the child would gain access
// to tools its governing parent cannot itself use.
func ValidateScope(child, parent *manifest.AgentManifest, cfg *policy.Config) error {
	if !cfg.Policies.ToolAuthorization.Enabled {
		return nil
	}

	parentTools := toolsForRole(parent.Authorization.Role, cfg)
	if len(parentTools) == 0 {
		// Parent has no tool restriction — any child scope is valid.
		return nil
	}

	childTools := toolsForRole(child.Authorization.Role, cfg)
	// Unknown child role → empty allowed set → subset check passes trivially.
	// The ToolAuthorizationValidator will block actual calls for undefined roles.

	parentSet := make(map[string]struct{}, len(parentTools))
	for _, t := range parentTools {
		parentSet[t] = struct{}{}
	}
	for _, t := range childTools {
		if _, ok := parentSet[t]; !ok {
			return fmt.Errorf(
				"child role %q includes tool %q not permitted by parent role %q",
				child.Authorization.Role, t, parent.Authorization.Role,
			)
		}
	}
	return nil
}

// toolsForRole looks up the AllowedTools list for a named role in the policy.
// Returns nil when the role is not defined.
func toolsForRole(role string, cfg *policy.Config) []string {
	for _, r := range cfg.Policies.ToolAuthorization.AgentRoles {
		if r.Role == role {
			return r.AllowedTools
		}
	}
	return nil
}
