// Package api provides the PerchGuard management REST API.
// All /api/* endpoints require Authorization: Bearer <PERCHGUARD_API_KEY>.
// Agent endpoints (/intercept, /validate/output, /agents/register) are unauthenticated.
package api

import (
	"net/http"
	"sync/atomic"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/quota"
	"github.com/Droshow/PerchGuard/perchguard/pkg/agent"
	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
	"github.com/Droshow/PerchGuard/perchguard/pkg/llm"
	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/review"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// APIServer exposes management endpoints for PerchGuard operators.
type APIServer struct {
	sessions        store.SessionStore
	auditRing       *store.AuditRingBuffer
	interceptor     *admission.Interceptor
	fleet           *agent.FleetManager
	llmClient       llm.Client
	policyMeta      *atomic.Pointer[policy.LoadResult] // updated on hot-reload
	manifestStore   *manifest.Store
	contextProvider audit.ContextProvider
	auditSink       audit.Sink
	keyStore        *KeyStore
	delegationStore *agent.DelegationStore
	budgetChecker   *quota.SessionBudgetChecker // nil when sessionBudget.enabled is false
	reviewStore     *review.Store               // in-process HITL pending review registry
	decisionStore   *store.SQLiteAuditSink      // durable per-decision store; nil when SQLite is unavailable
}

// NewAPIServer wires up the management API.
// policyMeta is an atomic pointer kept current by the policy watcher in cmd/main.go.
// keyStore holds the active API keys; all /api/* requests require a valid Bearer token.
func NewAPIServer(
	sessions store.SessionStore,
	auditRing *store.AuditRingBuffer,
	interceptor *admission.Interceptor,
	fleet *agent.FleetManager,
	llmClient llm.Client,
	policyMeta *atomic.Pointer[policy.LoadResult],
	manifestStore *manifest.Store,
	contextProvider audit.ContextProvider,
	auditSink audit.Sink,
	keyStore *KeyStore,
	delegationStore *agent.DelegationStore,
	budgetChecker *quota.SessionBudgetChecker,
	decisionStore *store.SQLiteAuditSink,
) *APIServer {
	return &APIServer{
		sessions:        sessions,
		auditRing:       auditRing,
		interceptor:     interceptor,
		fleet:           fleet,
		llmClient:       llmClient,
		policyMeta:      policyMeta,
		manifestStore:   manifestStore,
		contextProvider: contextProvider,
		auditSink:       auditSink,
		keyStore:        keyStore,
		delegationStore: delegationStore,
		budgetChecker:   budgetChecker,
		reviewStore:     review.NewStore(),
		decisionStore:   decisionStore,
	}
}

// RegisterRoutes mounts all /api/* handlers onto mux.
// All /api/* routes are wrapped with API key auth.
// Agent endpoints (/agents/register, /agents/{id}) are unauthenticated — called by agents.
func (s *APIServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.serveDashboard)
	guard := requireAPIKey(s.keyStore)
	mux.Handle("GET /api/sessions", guard(http.HandlerFunc(s.listSessions)))
	mux.Handle("GET /api/sessions/{id}", guard(http.HandlerFunc(s.getSession)))
	mux.Handle("GET /api/sessions/{id}/budget", guard(http.HandlerFunc(s.getSessionBudget)))
	mux.Handle("DELETE /api/sessions/{id}", guard(http.HandlerFunc(s.deleteSession)))
	mux.Handle("GET /api/audit", guard(http.HandlerFunc(s.queryAudit)))
	mux.Handle("GET /api/pipeline", guard(http.HandlerFunc(s.getPipeline)))
	mux.Handle("GET /api/stats", guard(http.HandlerFunc(s.getStats)))
	mux.Handle("POST /api/query", guard(http.HandlerFunc(s.queryLLM)))
	mux.Handle("GET /api/fleet/summary", guard(http.HandlerFunc(s.getFleetSummary)))
	mux.Handle("GET /api/keys", guard(http.HandlerFunc(s.listKeys)))
	mux.Handle("POST /api/keys/rotate", guard(http.HandlerFunc(s.rotateKey)))
	mux.HandleFunc("POST /agents/register", s.registerAgent)
	mux.HandleFunc("GET /agents/{id}", s.getAgent)
	s.registerReviewRoutes(mux)
}
