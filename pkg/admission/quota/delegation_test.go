package quota

import (
	"context"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// stubSessionStore is a minimal in-memory SessionStore for testing delegation budget.
type stubSessionStore struct {
	sessions map[string]*store.SessionState
}

func newStubStore(sessions map[string]*store.SessionState) store.SessionStore {
	return &stubSessionStore{sessions: sessions}
}

func (s *stubSessionStore) Get(id string) (*store.SessionState, bool) {
	ss, ok := s.sessions[id]
	return ss, ok
}
func (s *stubSessionStore) Set(id string, ss *store.SessionState) { s.sessions[id] = ss }
func (s *stubSessionStore) Delete(id string)                      { delete(s.sessions, id) }
func (s *stubSessionStore) List() []*store.SessionState {
	out := make([]*store.SessionState, 0, len(s.sessions))
	for _, ss := range s.sessions {
		out = append(out, ss)
	}
	return out
}
func (s *stubSessionStore) Children(parentID string) []string {
	var out []string
	for id, ss := range s.sessions {
		if ss.ParentSessionID == parentID {
			out = append(out, id)
		}
	}
	return out
}

func budgetPolicy(maxCalls int) policy.SessionBudgetPolicy {
	return policy.SessionBudgetPolicy{
		Enabled: true,
		Limits: policy.BudgetLimits{
			MaxTokensPerSession:    1_000_000,
			MaxCostPerSessionUSD:   100,
			MaxToolCallsPerSession: maxCalls,
		},
	}
}

func intercept(sessionID string) *admission.ToolCallAdmissionRequest {
	return &admission.ToolCallAdmissionRequest{
		UID:       "uid-" + sessionID,
		SessionID: sessionID,
		ToolCall:  admission.ToolCall{Name: "read_file"},
	}
}

// TestDelegatedCallLimit_UsedOverPolicyDefault confirms that when a session has a
// DelegatedCallLimit set, it overrides the policy's MaxToolCallsPerSession.
func TestDelegatedCallLimit_UsedOverPolicyDefault(t *testing.T) {
	st := newStubStore(map[string]*store.SessionState{
		"child-sess": {
			SessionID:          "child-sess",
			ParentSessionID:    "parent-sess",
			DelegatedCallLimit: 5, // much lower than policy default of 200
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		},
	})

	c := NewSessionBudgetChecker(budgetPolicy(200), st)
	req := intercept("child-sess")

	// First 5 calls should pass.
	for i := 0; i < 5; i++ {
		if v := c.Check(context.Background(), req); v != nil {
			t.Fatalf("call %d: want nil, got %v", i+1, v)
		}
		c.Record(context.Background(), req)
	}

	// 6th call must be blocked by the delegated limit (5), not the policy limit (200).
	v := c.Check(context.Background(), req)
	if v == nil {
		t.Fatal("want violation after delegated limit reached, got nil")
	}
	if v.Policy != "sessionBudget.maxCalls" {
		t.Errorf("want policy sessionBudget.maxCalls, got %s", v.Policy)
	}
}

// TestNoDelegatedLimit_FallsBackToPolicy confirms root sessions (no DelegatedCallLimit)
// use the policy default.
func TestNoDelegatedLimit_FallsBackToPolicy(t *testing.T) {
	st := newStubStore(map[string]*store.SessionState{
		"root-sess": {
			SessionID: "root-sess",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			// DelegatedCallLimit is zero — use policy default
		},
	})

	c := NewSessionBudgetChecker(budgetPolicy(3), st)
	req := intercept("root-sess")

	for i := 0; i < 3; i++ {
		if v := c.Check(context.Background(), req); v != nil {
			t.Fatalf("call %d: want nil, got %v", i+1, v)
		}
		c.Record(context.Background(), req)
	}

	if v := c.Check(context.Background(), req); v == nil {
		t.Fatal("want violation at policy limit (3), got nil")
	}
}

// TestNilStore_NoError confirms that passing a nil store (non-delegation deployment)
// doesn't panic and uses policy default.
func TestNilStore_NoError(t *testing.T) {
	c := NewSessionBudgetChecker(budgetPolicy(2), nil)
	req := intercept("any-sess")

	c.Record(context.Background(), req)
	c.Record(context.Background(), req)

	if v := c.Check(context.Background(), req); v == nil {
		t.Fatal("want violation at policy limit with nil store, got nil")
	}
}
