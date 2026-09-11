package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/quota"
	"github.com/Droshow/PerchGuard/perchguard/pkg/agent"
	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
	"github.com/Droshow/PerchGuard/perchguard/pkg/llm"
	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

type noOpAuditLog struct{}

func (noOpAuditLog) Log(admission.AuditEntry) {}

type stubLLM struct{ answer string }

func (s *stubLLM) Complete(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{Content: s.answer}, nil
}

func buildServer(t *testing.T, apiKey string, llmClient llm.Client) (*APIServer, *store.MemoryStore, *store.AuditRingBuffer) {
	t.Helper()
	sessions := store.NewMemoryStore()
	ring := store.NewAuditRingBuffer(100)
	interceptor := admission.NewInterceptor(nil, nil, nil, noOpAuditLog{})
	fleet := agent.NewFleetManager(agent.AgentFleetConfig{}, sessions)
	manifestStore := manifest.NewStore()
	var policyMeta atomic.Pointer[policy.LoadResult]
	srv := NewAPIServer(
		sessions, ring, interceptor, fleet,
		llmClient,
		&policyMeta,
		manifestStore,
		audit.NoOpContextProvider{},
		audit.NoOpSink{},
		NewKeyStore(apiKey, "default"),
		agent.NewDelegationStore(),
		nil, // budgetChecker — nil is valid; budget endpoint returns 503 when disabled
		nil, // decisionStore — nil is valid; review approve/deny just skips the audit write-back
	)
	return srv, sessions, ring
}

func buildMux(srv *APIServer) *http.ServeMux {
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return mux
}

func do(t *testing.T, mux http.Handler, method, path, apiKey string, body io.Reader) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr.Result()
}

// ── requireAPIKey middleware ──────────────────────────────────────────────────

func TestRequireAPIKey_NoHeader(t *testing.T) {
	srv, _, _ := buildServer(t, "test-key", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401 without auth header, got %d", resp.StatusCode)
	}
}

func TestRequireAPIKey_WrongKey(t *testing.T) {
	srv, _, _ := buildServer(t, "correct-key", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions", "wrong-key", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("want 401 for wrong key, got %d", resp.StatusCode)
	}
}

func TestRequireAPIKey_CorrectKey(t *testing.T) {
	srv, _, _ := buildServer(t, "secret", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions", "secret", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200 for correct key, got %d", resp.StatusCode)
	}
}

// Subtle timing-safe check: raw key without "Bearer " prefix must be rejected.
func TestRequireAPIKey_NoBearerPrefix(t *testing.T) {
	srv, _, _ := buildServer(t, "mykey", nil)
	mux := buildMux(srv)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "mykey")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Result().StatusCode != http.StatusUnauthorized {
		t.Error("want 401 when Bearer prefix is absent")
	}
}

// ── GET /api/sessions ─────────────────────────────────────────────────────────

func TestListSessions_Empty(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions", "k", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var out []any
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 0 {
		t.Errorf("want empty array, got %d items", len(out))
	}
}

func TestListSessions_WithSessions(t *testing.T) {
	srv, sessions, _ := buildServer(t, "k", nil)
	sessions.Set("s1", &store.SessionState{SessionID: "s1", RiskScore: 0.2, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	sessions.Set("s2", &store.SessionState{SessionID: "s2", RiskScore: 0.8, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions", "k", nil)
	var out []map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 2 {
		t.Errorf("want 2 sessions, got %d", len(out))
	}
}

// ── GET /api/sessions/{id} ────────────────────────────────────────────────────

func TestGetSession_NotFound(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions/doesnotexist", "k", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestGetSession_Found(t *testing.T) {
	srv, sessions, _ := buildServer(t, "k", nil)
	sessions.Set("abc", &store.SessionState{
		SessionID: "abc",
		RiskScore: 0.5,
		Events: []store.ToolEvent{
			{Tool: "read_file", Stage: "recon", Timestamp: time.Now()},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions/abc", "k", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var detail map[string]any
	json.NewDecoder(resp.Body).Decode(&detail)
	if detail["session_id"] != "abc" {
		t.Errorf("want session_id=abc, got %v", detail["session_id"])
	}
	if detail["event_count"].(float64) != 1 {
		t.Errorf("want event_count=1, got %v", detail["event_count"])
	}
}

// ── DELETE /api/sessions/{id} ─────────────────────────────────────────────────

func TestDeleteSession_NotFound(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodDelete, "/api/sessions/ghost", "k", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestDeleteSession_Evicts(t *testing.T) {
	srv, sessions, _ := buildServer(t, "k", nil)
	sessions.Set("del", &store.SessionState{SessionID: "del", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodDelete, "/api/sessions/del", "k", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if _, ok := sessions.Get("del"); ok {
		t.Error("session should be evicted after DELETE")
	}
}

// ── GET /api/audit ────────────────────────────────────────────────────────────

func TestQueryAudit_EmptyResult(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/audit", "k", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var out []any
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 0 {
		t.Errorf("want empty array, got %d items", len(out))
	}
}

func TestQueryAudit_DecisionFilter(t *testing.T) {
	srv, _, ring := buildServer(t, "k", nil)
	ring.Push(store.AuditRecord{Decision: "ALLOW", ToolName: "t1", Timestamp: time.Now()})
	ring.Push(store.AuditRecord{Decision: "DENY", ToolName: "t2", Timestamp: time.Now()})
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/audit?decision=DENY", "k", nil)
	var out []map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out) != 1 || out[0]["decision"] != "DENY" {
		t.Errorf("want 1 DENY record, got %v", out)
	}
}

// ── GET /api/stats ────────────────────────────────────────────────────────────

func TestGetStats_DecisionCounts(t *testing.T) {
	srv, sessions, ring := buildServer(t, "k", nil)
	sessions.Set("s1", &store.SessionState{SessionID: "s1", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	ring.Push(store.AuditRecord{Decision: "ALLOW", ToolName: "t1", Timestamp: time.Now()})
	ring.Push(store.AuditRecord{Decision: "DENY", ToolName: "exec_cmd", Timestamp: time.Now()})
	ring.Push(store.AuditRecord{Decision: "DENY", ToolName: "exec_cmd", Timestamp: time.Now()})
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/stats", "k", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var stats map[string]any
	json.NewDecoder(resp.Body).Decode(&stats)
	if stats["total_sessions"].(float64) != 1 {
		t.Errorf("want total_sessions=1, got %v", stats["total_sessions"])
	}
	decisions := stats["decisions"].(map[string]any)
	if decisions["DENY"].(float64) != 2 {
		t.Errorf("want 2 DENY decisions, got %v", decisions["DENY"])
	}
}

func TestGetStats_TopDeniedTools(t *testing.T) {
	srv, _, ring := buildServer(t, "k", nil)
	for i := 0; i < 3; i++ {
		ring.Push(store.AuditRecord{Decision: "DENY", ToolName: "dangerous_tool", Timestamp: time.Now()})
	}
	ring.Push(store.AuditRecord{Decision: "DENY", ToolName: "other_tool", Timestamp: time.Now()})
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/stats", "k", nil)
	var stats map[string]any
	json.NewDecoder(resp.Body).Decode(&stats)
	topDenied := stats["top_denied_tools"].([]any)
	if len(topDenied) == 0 {
		t.Fatal("want at least 1 top denied tool")
	}
	top := topDenied[0].(map[string]any)
	if top["tool"] != "dangerous_tool" {
		t.Errorf("want dangerous_tool first by frequency, got %v", top["tool"])
	}
}

// ── GET /api/fleet/summary ────────────────────────────────────────────────────

func TestGetFleetSummary_RiskDistribution(t *testing.T) {
	srv, sessions, ring := buildServer(t, "k", nil)
	sessions.Set("low", &store.SessionState{SessionID: "low", RiskScore: 0.1, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	sessions.Set("med", &store.SessionState{SessionID: "med", RiskScore: 0.5, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	sessions.Set("hi", &store.SessionState{SessionID: "hi", RiskScore: 0.9, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	ring.Push(store.AuditRecord{Decision: "TERMINATE", SessionID: "hi", Reason: "risk threshold", RiskScore: 0.9, Timestamp: time.Now()})
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/fleet/summary", "k", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var summary map[string]any
	json.NewDecoder(resp.Body).Decode(&summary)
	if summary["active_sessions"].(float64) != 3 {
		t.Errorf("want 3 active sessions, got %v", summary["active_sessions"])
	}
	if summary["high_risk_sessions"].(float64) != 1 {
		t.Errorf("want 1 high-risk session, got %v", summary["high_risk_sessions"])
	}
	dist := summary["risk_distribution"].(map[string]any)
	if dist["low"].(float64) != 1 || dist["medium"].(float64) != 1 || dist["high"].(float64) != 1 {
		t.Errorf("unexpected risk distribution: %v", dist)
	}
	terminates := summary["recent_terminates"].([]any)
	if len(terminates) != 1 {
		t.Errorf("want 1 recent terminate, got %d", len(terminates))
	}
}

// ── GET /api/pipeline ─────────────────────────────────────────────────────────

func TestGetPipeline_HasRequiredKeys(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/pipeline", "k", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var pipeline map[string]any
	json.NewDecoder(resp.Body).Decode(&pipeline)
	for _, key := range []string{"validators", "mutators", "quotas", "semantic_firewall_enabled"} {
		if _, ok := pipeline[key]; !ok {
			t.Errorf("pipeline response missing key: %s", key)
		}
	}
}

// ── POST /agents/register ─────────────────────────────────────────────────────

const minimalManifest = `{
  "apiVersion": "perchguard/v1",
  "kind": "AgentManifest",
  "metadata": {"id": "test-agent", "owner": "test", "created": "2026-05-07", "version": "1.0"},
  "mission": {"summary": "unit test agent", "scope": ["read"], "out_of_scope": []},
  "authorization": {"role": "readonly", "allowed_systems": [], "human_review_required_for": []},
  "invariants": [],
  "project_context": {}
}`

func TestRegisterAgent_Valid(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodPost, "/agents/register", "", bytes.NewBufferString(minimalManifest))
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 201, got %d: %s", resp.StatusCode, body)
	}
	var reg map[string]any
	json.NewDecoder(resp.Body).Decode(&reg)
	if reg["agent_id"] != "test-agent" {
		t.Errorf("want agent_id=test-agent, got %v", reg["agent_id"])
	}
	if reg["session_id"] == nil || reg["session_id"] == "" {
		t.Error("want non-empty session_id")
	}
	if reg["token"] == nil || reg["token"] == "" {
		t.Error("want non-empty token")
	}
}

func TestRegisterAgent_InvalidBody(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodPost, "/agents/register", "", bytes.NewBufferString("not json or yaml"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400 for invalid manifest, got %d", resp.StatusCode)
	}
}

// Each registration must produce a unique session_id and token.
func TestRegisterAgent_UniqueTokensPerRegistration(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	var tokens, sessions []string
	for i := 0; i < 3; i++ {
		resp := do(t, mux, http.MethodPost, "/agents/register", "", bytes.NewBufferString(minimalManifest))
		var reg map[string]any
		json.NewDecoder(resp.Body).Decode(&reg)
		tokens = append(tokens, reg["token"].(string))
		sessions = append(sessions, reg["session_id"].(string))
	}
	for i := 0; i < len(tokens); i++ {
		for j := i + 1; j < len(tokens); j++ {
			if tokens[i] == tokens[j] {
				t.Errorf("duplicate token at indices %d and %d", i, j)
			}
			if sessions[i] == sessions[j] {
				t.Errorf("duplicate session_id at indices %d and %d", i, j)
			}
		}
	}
}

// ── Delegated registration (parent_session_id) ────────────────────────────────

func childManifest(parentSessionID string) string {
	return `{
  "apiVersion": "perchguard/v1",
  "kind": "AgentManifest",
  "metadata": {"id": "child-agent", "owner": "test", "created": "2026-05-07", "version": "1.0"},
  "mission": {"summary": "child agent", "scope": ["read"], "out_of_scope": []},
  "authorization": {"role": "readonly", "allowed_systems": [], "human_review_required_for": []},
  "invariants": [],
  "project_context": {},
  "parent_session_id": "` + parentSessionID + `"
}`
}

// A registration asserting parent_session_id without presenting that parent's own
// token must be rejected — otherwise parent_session_id is just an unverified claim.
func TestRegisterAgent_DelegationRequiresValidParentToken(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)

	parentResp := do(t, mux, http.MethodPost, "/agents/register", "", bytes.NewBufferString(minimalManifest))
	var parentReg map[string]any
	json.NewDecoder(parentResp.Body).Decode(&parentReg)
	parentSessionID := parentReg["session_id"].(string)

	cases := []struct {
		name  string
		token string
	}{
		{"missing token", ""},
		{"wrong token", "pgat-not-the-real-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/agents/register", bytes.NewBufferString(childManifest(parentSessionID)))
			req.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				req.Header.Set("X-PerchGuard-Agent-Token", tc.token)
			}
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			resp := rr.Result()
			if resp.StatusCode != http.StatusForbidden {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("want 403, got %d: %s", resp.StatusCode, body)
			}
		})
	}
}

// A registration presenting the parent's own token succeeds and records the edge.
func TestRegisterAgent_DelegationAcceptsValidParentToken(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)

	parentResp := do(t, mux, http.MethodPost, "/agents/register", "", bytes.NewBufferString(minimalManifest))
	var parentReg map[string]any
	json.NewDecoder(parentResp.Body).Decode(&parentReg)
	parentSessionID := parentReg["session_id"].(string)
	parentToken := parentReg["token"].(string)

	req := httptest.NewRequest(http.MethodPost, "/agents/register", bytes.NewBufferString(childManifest(parentSessionID)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PerchGuard-Agent-Token", parentToken)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	resp := rr.Result()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 201, got %d: %s", resp.StatusCode, body)
	}
	var childReg map[string]any
	json.NewDecoder(resp.Body).Decode(&childReg)
	childSessionID := childReg["session_id"].(string)

	if got, ok := srv.delegationStore.Parent(childSessionID); !ok || got != parentSessionID {
		t.Errorf("want delegation edge %s -> %s, got %s (ok=%v)", childSessionID, parentSessionID, got, ok)
	}
}

// Budget inheritance (pkg/api/agents.go's delegationFraction logic) is computed
// independently per child registration — there is no pooled/conserved budget across
// concurrent siblings. This documents that as current, accepted behavior: three
// siblings each independently get 50% of the parent's limit, so their sum (150) can
// exceed the parent's own nominal limit (100). Real pooling would be a
// pkg/agent/fleet.go change; out of scope for the LangGraph delegation work this
// guards against regressing silently.
func TestRegisterAgent_DelegationBudgetNotPooledAcrossSiblings(t *testing.T) {
	srv, sessions, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)

	cfg := &policy.Config{
		Policies: policy.Policies{
			ToolAuthorization: policy.ToolAuthorizationPolicy{Enabled: false},
			SessionBudget: policy.SessionBudgetPolicy{
				Enabled:            true,
				Limits:             policy.BudgetLimits{MaxToolCallsPerSession: 100},
				DelegationFraction: 0.5,
			},
		},
	}
	srv.policyMeta.Store(&policy.LoadResult{Config: cfg})

	parentResp := do(t, mux, http.MethodPost, "/agents/register", "", bytes.NewBufferString(minimalManifest))
	var parentReg map[string]any
	json.NewDecoder(parentResp.Body).Decode(&parentReg)
	parentSessionID := parentReg["session_id"].(string)
	parentToken := parentReg["token"].(string)

	var childLimits []int
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/agents/register", bytes.NewBufferString(childManifest(parentSessionID)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-PerchGuard-Agent-Token", parentToken)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		resp := rr.Result()
		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("sibling %d: want 201, got %d: %s", i, resp.StatusCode, body)
		}
		var childReg map[string]any
		json.NewDecoder(resp.Body).Decode(&childReg)
		childSessionID := childReg["session_id"].(string)
		ss, ok := sessions.Get(childSessionID)
		if !ok {
			t.Fatalf("sibling %d: no session state found", i)
		}
		childLimits = append(childLimits, ss.DelegatedCallLimit)
	}

	sum := 0
	for i, limit := range childLimits {
		if limit != 50 {
			t.Errorf("sibling %d: want DelegatedCallLimit=50 (0.5 * parent's 100), got %d", i, limit)
		}
		sum += limit
	}
	if sum <= cfg.Policies.SessionBudget.Limits.MaxToolCallsPerSession {
		t.Fatalf("expected this test to demonstrate unpooled budget (sum %d > parent limit %d) — "+
			"if this now fails, budget pooling may have been added and this test's premise is stale",
			sum, cfg.Policies.SessionBudget.Limits.MaxToolCallsPerSession)
	}
}

// ── GET /agents/{id} ──────────────────────────────────────────────────────────

func TestGetAgent_NotFound(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/agents/nonexistent", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestGetAgent_FoundAfterRegister(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	regResp := do(t, mux, http.MethodPost, "/agents/register", "", bytes.NewBufferString(minimalManifest))
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("registration failed: %d", regResp.StatusCode)
	}
	var reg map[string]any
	json.NewDecoder(regResp.Body).Decode(&reg)
	sessionID := reg["session_id"].(string)

	getResp := do(t, mux, http.MethodGet, "/agents/"+sessionID, "", nil)
	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("want 200, got %d: %s", getResp.StatusCode, body)
	}
	var agentDetail map[string]any
	json.NewDecoder(getResp.Body).Decode(&agentDetail)
	if agentDetail["agent_id"] != "test-agent" {
		t.Errorf("want agent_id=test-agent, got %v", agentDetail["agent_id"])
	}
}

// ── POST /api/query ───────────────────────────────────────────────────────────

func TestQueryLLM_DisabledReturns503(t *testing.T) {
	srv, _, _ := buildServer(t, "k", nil)
	mux := buildMux(srv)
	body := bytes.NewBufferString(`{"question":"what happened?"}`)
	resp := do(t, mux, http.MethodPost, "/api/query", "k", body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("want 503 when LLM not configured, got %d", resp.StatusCode)
	}
}

func TestQueryLLM_AnswersWhenEnabled(t *testing.T) {
	srv, _, _ := buildServer(t, "k", &stubLLM{answer: "looks clean"})
	mux := buildMux(srv)
	body := bytes.NewBufferString(`{"question":"any anomalies?"}`)
	resp := do(t, mux, http.MethodPost, "/api/query", "k", body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, b)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	if out["answer"] != "looks clean" {
		t.Errorf("unexpected answer: %v", out["answer"])
	}
}

// ── GET /api/sessions/{id}/budget ────────────────────────────────────────────

func buildServerWithBudget(t *testing.T, apiKey string, checker *quota.SessionBudgetChecker) (*APIServer, *store.MemoryStore) {
	t.Helper()
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
		checker,
		nil, // decisionStore
	)
	return srv, sessions
}

func TestGetSessionBudget_NoBudgetChecker(t *testing.T) {
	srv, _ := buildServerWithBudget(t, "k", nil)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions/any-session/budget", "k", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("want 503 when budget checker not configured, got %d", resp.StatusCode)
	}
}

func TestGetSessionBudget_UnknownSession(t *testing.T) {
	checker := quota.NewSessionBudgetChecker(policy.SessionBudgetPolicy{
		Enabled: true,
		Limits:  policy.BudgetLimits{MaxTokensPerSession: 500000, MaxCostPerSessionUSD: 5.0, MaxToolCallsPerSession: 200},
	}, nil)
	srv, _ := buildServerWithBudget(t, "k", checker)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions/ghost-session/budget", "k", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404 for session with no calls, got %d", resp.StatusCode)
	}
}

func TestGetSessionBudget_KnownSession(t *testing.T) {
	p := policy.SessionBudgetPolicy{
		Enabled: true,
		Limits: policy.BudgetLimits{
			MaxTokensPerSession:    500000,
			MaxCostPerSessionUSD:   5.0,
			MaxToolCallsPerSession: 200,
			MaxToolCallsPerMinute:  30,
		},
	}
	checker := quota.NewSessionBudgetChecker(p, nil)

	// Seed two tool calls with token metadata so the checker has state to return.
	req := &admission.ToolCallAdmissionRequest{
		SessionID: "copilot-s1",
		ToolCall:  admission.ToolCall{Name: "read_file"},
		Metadata:  map[string]string{"tokens_used": "1000", "model": "claude-sonnet-4-6"},
	}
	checker.Record(context.Background(), req)
	checker.Record(context.Background(), req)

	srv, _ := buildServerWithBudget(t, "k", checker)
	mux := buildMux(srv)
	resp := do(t, mux, http.MethodGet, "/api/sessions/copilot-s1/budget", "k", nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 200, got %d: %s", resp.StatusCode, b)
	}

	var snap map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if snap["session_id"] != "copilot-s1" {
		t.Errorf("want session_id copilot-s1, got %v", snap["session_id"])
	}
	if snap["tool_calls"].(float64) != 2 {
		t.Errorf("want tool_calls 2, got %v", snap["tool_calls"])
	}
	if snap["estimated_tokens"].(float64) != 2000 {
		t.Errorf("want estimated_tokens 2000, got %v", snap["estimated_tokens"])
	}
	if snap["estimated_cost_usd"].(float64) <= 0 {
		t.Errorf("want cost > 0, got %v", snap["estimated_cost_usd"])
	}
	limits, ok := snap["limits"].(map[string]any)
	if !ok {
		t.Fatal("limits field missing or wrong type")
	}
	if limits["maxTokensPerSession"].(float64) != 500000 {
		t.Errorf("limits.maxTokensPerSession: want 500000, got %v", limits["maxTokensPerSession"])
	}
}
