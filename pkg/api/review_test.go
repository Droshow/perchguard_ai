package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/agent"
	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// buildServerWithDecisionStore mirrors buildServer but wires a real SQLite audit
// store, so the review approve/deny -> MarkReviewed write-back can be observed.
func buildServerWithDecisionStore(t *testing.T, apiKey string) (*APIServer, *store.SQLiteAuditSink) {
	t.Helper()
	sink, err := store.NewSQLiteAuditSink(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("NewSQLiteAuditSink: %v", err)
	}
	t.Cleanup(func() { sink.Close() })

	sessions := store.NewMemoryStore()
	ring := store.NewAuditRingBuffer(100)
	interceptor := admission.NewInterceptor(nil, nil, nil, noOpAuditLog{})
	fleet := agent.NewFleetManager(agent.AgentFleetConfig{}, sessions)
	manifestStore := manifest.NewStore()
	var policyMeta atomic.Pointer[policy.LoadResult]
	srv := NewAPIServer(
		sessions, ring, interceptor, fleet,
		nil, &policyMeta, manifestStore,
		audit.NoOpContextProvider{}, audit.NoOpSink{},
		NewKeyStore(apiKey, "default"),
		agent.NewDelegationStore(),
		nil, // budgetChecker
		sink,
	)
	return srv, sink
}

func submitPendingReview(t *testing.T, mux http.Handler, requestUID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"session_id":  "sess-1",
		"agent_id":    "agent-1",
		"tool_name":   "write_file",
		"risk_score":  0.75,
		"reason":      "elevated risk",
		"request_uid": requestUID,
	})
	resp := do(t, mux, http.MethodPost, "/api/review", "", bytes.NewReader(body))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit review: want 200, got %d", resp.StatusCode)
	}
	var out struct{ ID string `json:"id"` }
	json.NewDecoder(resp.Body).Decode(&out)
	return out.ID
}

func TestApproveReview_WritesBackToAuditRecord(t *testing.T) {
	srv, sink := buildServerWithDecisionStore(t, "test-key")
	mux := buildMux(srv)

	const requestUID = "req-approve-1"
	sink.Push(store.AuditRecord{
		RequestUID: requestUID, SessionID: "sess-1", AgentID: "agent-1",
		ToolName: "write_file", Decision: "HUMAN_REVIEW", Timestamp: time.Now(),
	})

	id := submitPendingReview(t, mux, requestUID)

	body, _ := json.Marshal(map[string]string{"reviewed_by": "alice"})
	resp := do(t, mux, http.MethodPost, "/api/review/"+id+"/approve", "test-key", bytes.NewReader(body))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("approve: want 204, got %d", resp.StatusCode)
	}

	recs, err := sink.QueryDecisions(store.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("QueryDecisions: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 decision record, got %d", len(recs))
	}
	if recs[0].ApprovalStatus != "APPROVED" || recs[0].ReviewedBy != "alice" {
		t.Errorf("approval not written back: got ApprovalStatus=%q ReviewedBy=%q", recs[0].ApprovalStatus, recs[0].ReviewedBy)
	}
	if recs[0].ReviewedAt == nil {
		t.Error("expected ReviewedAt to be set")
	}
}

func TestDenyReview_WritesBackToAuditRecord(t *testing.T) {
	srv, sink := buildServerWithDecisionStore(t, "test-key")
	mux := buildMux(srv)

	const requestUID = "req-deny-1"
	sink.Push(store.AuditRecord{
		RequestUID: requestUID, SessionID: "sess-1", AgentID: "agent-1",
		ToolName: "write_file", Decision: "HUMAN_REVIEW", Timestamp: time.Now(),
	})

	id := submitPendingReview(t, mux, requestUID)

	resp := do(t, mux, http.MethodPost, "/api/review/"+id+"/deny", "test-key", bytes.NewReader([]byte("{}")))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("deny: want 204, got %d", resp.StatusCode)
	}

	recs, err := sink.QueryDecisions(store.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("QueryDecisions: %v", err)
	}
	if recs[0].ApprovalStatus != "DENIED" {
		t.Errorf("expected ApprovalStatus=DENIED, got %q", recs[0].ApprovalStatus)
	}
}

func TestApproveReview_NoRequestUID_SkipsWriteBack(t *testing.T) {
	srv, sink := buildServerWithDecisionStore(t, "test-key")
	mux := buildMux(srv)

	// No matching audit record at all — submitted review carries no request_uid.
	id := submitPendingReview(t, mux, "")

	resp := do(t, mux, http.MethodPost, "/api/review/"+id+"/approve", "test-key", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("approve: want 204, got %d", resp.StatusCode)
	}

	recs, err := sink.QueryDecisions(store.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("QueryDecisions: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected no audit records (none were ever pushed), got %d", len(recs))
	}
}

func TestApproveReview_UnknownID(t *testing.T) {
	srv, _ := buildServerWithDecisionStore(t, "test-key")
	mux := buildMux(srv)

	resp := do(t, mux, http.MethodPost, "/api/review/does-not-exist/approve", "test-key", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404 for unknown review id, got %d", resp.StatusCode)
	}
}
