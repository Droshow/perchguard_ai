package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/agent"
	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// buildServerWithLineage mirrors buildServer but wires in a real LineageStore, needed
// to test the InheritedDataRefs verification path and GET /api/swarm.
func buildServerWithLineage(t *testing.T, apiKey string) (*APIServer, *store.LineageStore) {
	t.Helper()
	sessions := store.NewMemoryStore()
	ring := store.NewAuditRingBuffer(100)
	interceptor := admission.NewInterceptor(nil, nil, nil, noOpAuditLog{})
	fleet := agent.NewFleetManager(agent.AgentFleetConfig{}, sessions)
	manifestStore := manifest.NewStore()
	lineageStore := store.NewLineageStore()
	var policyMeta atomic.Pointer[policy.LoadResult]
	srv := NewAPIServer(
		sessions, ring, interceptor, fleet,
		nil,
		&policyMeta,
		manifestStore,
		audit.NoOpContextProvider{},
		audit.NoOpSink{},
		NewKeyStore(apiKey, "default"),
		agent.NewDelegationStore(),
		nil, // budgetChecker
		nil, // decisionStore
		lineageStore,
	)
	return srv, lineageStore
}

func childManifestWithInheritedRefs(parentSessionID string, refs []string) string {
	refsJSON, _ := json.Marshal(refs)
	return `{
  "apiVersion": "perchguard/v1",
  "kind": "AgentManifest",
  "metadata": {"id": "child-agent", "owner": "test", "created": "2026-05-07", "version": "1.0"},
  "mission": {"summary": "child agent", "scope": ["read"], "out_of_scope": []},
  "authorization": {"role": "readonly", "allowed_systems": [], "human_review_required_for": []},
  "invariants": [],
  "project_context": {},
  "parent_session_id": "` + parentSessionID + `",
  "inherited_data_refs": ` + string(refsJSON) + `
}`
}

func registerParent(t *testing.T, mux http.Handler) (sessionID, token string) {
	t.Helper()
	resp := do(t, mux, http.MethodPost, "/agents/register", "", bytes.NewBufferString(minimalManifest))
	var reg map[string]any
	json.NewDecoder(resp.Body).Decode(&reg)
	return reg["session_id"].(string), reg["token"].(string)
}

// A claimed inherited ref the parent never actually produced must be rejected —
// otherwise a sub-agent could assert arbitrary lineage and launder an exfiltration
// path around the DataExfiltrationValidator.
func TestRegisterAgent_InheritedDataRefs_RejectsUnverifiedClaim(t *testing.T) {
	srv, _ := buildServerWithLineage(t, "k")
	mux := buildMux(srv)

	parentSessionID, parentToken := registerParent(t, mux)

	req := httptest.NewRequest(http.MethodPost, "/agents/register",
		bytes.NewBufferString(childManifestWithInheritedRefs(parentSessionID, []string{"never-produced"})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PerchGuard-Agent-Token", parentToken)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	resp := rr.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for unverified inherited ref claim, got %d", resp.StatusCode)
	}
}

// A claimed ref that genuinely exists in the parent's lineage graph is accepted and
// seeded into the child's own graph.
func TestRegisterAgent_InheritedDataRefs_AcceptsVerifiedClaim(t *testing.T) {
	srv, lineageStore := buildServerWithLineage(t, "k")
	mux := buildMux(srv)

	parentSessionID, parentToken := registerParent(t, mux)
	lineageStore.AddEdge(parentSessionID, "refA", nil, "read_policy")

	req := httptest.NewRequest(http.MethodPost, "/agents/register",
		bytes.NewBufferString(childManifestWithInheritedRefs(parentSessionID, []string{"refA"})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PerchGuard-Agent-Token", parentToken)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	resp := rr.Result()
	if resp.StatusCode != http.StatusCreated {
		body := new(bytes.Buffer)
		body.ReadFrom(resp.Body)
		t.Fatalf("want 201, got %d: %s", resp.StatusCode, body.String())
	}

	var childReg map[string]any
	json.NewDecoder(resp.Body).Decode(&childReg)
	childSessionID := childReg["session_id"].(string)

	childGraph := lineageStore.Graph(childSessionID)
	if childGraph == nil || !childGraph.HasAny([]string{"refA"}) {
		t.Error("want refA seeded into child's lineage graph")
	}
}

func TestGetSwarmGraph_ReturnsDelegationAndDataEdges(t *testing.T) {
	srv, lineageStore := buildServerWithLineage(t, "k")
	mux := buildMux(srv)

	parentSessionID, parentToken := registerParent(t, mux)
	lineageStore.AddEdge(parentSessionID, "refA", nil, "read_policy")

	req := httptest.NewRequest(http.MethodPost, "/agents/register",
		bytes.NewBufferString(childManifestWithInheritedRefs(parentSessionID, []string{"refA"})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PerchGuard-Agent-Token", parentToken)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	var childReg map[string]any
	json.NewDecoder(rr.Result().Body).Decode(&childReg)
	childSessionID := childReg["session_id"].(string)

	// Child consumes the inherited ref — this is the edge the swarm graph should surface.
	lineageStore.AddEdge(childSessionID, "refB", []string{"refA"}, "send_email")

	resp := do(t, mux, http.MethodGet, "/api/swarm", "k", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var g swarmGraph
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(g.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d: %+v", len(g.Nodes), g.Nodes)
	}

	var sawDelegation, sawData bool
	for _, e := range g.Edges {
		if e.Type == "delegation" && e.From == parentSessionID && e.To == childSessionID {
			sawDelegation = true
		}
		if e.Type == "data" && e.FromSession == parentSessionID && e.ToSession == childSessionID &&
			e.FromTool == "read_policy" && e.ToTool == "send_email" && e.Ref == "refA" {
			sawData = true
		}
	}
	if !sawDelegation {
		t.Errorf("want delegation edge %s -> %s, got %+v", parentSessionID, childSessionID, g.Edges)
	}
	if !sawData {
		t.Errorf("want cross-session data edge for refA, got %+v", g.Edges)
	}
}
