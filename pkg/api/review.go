package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/review"
	"github.com/Droshow/PerchGuard/perchguard/pkg/telemetry"
)

// submitReviewRequest is the body sent by the claude-hook when a tool call
// scores in the REVIEW band. The hook then long-polls waitReview.
type submitReviewRequest struct {
	SessionID string         `json:"session_id"`
	AgentID   string         `json:"agent_id"`
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	RiskScore float64        `json:"risk_score"`
	Reason    string         `json:"reason"`
	// RequestUID ties this pending review back to the admission decision's audit
	// record, so the operator's eventual approve/deny is written back onto it.
	RequestUID string `json:"request_uid,omitempty"`
}

type submitReviewResponse struct {
	ID string `json:"id"`
}

// submitReview handles POST /api/review (unauthenticated — called by the hook process).
func (s *APIServer) submitReview(w http.ResponseWriter, r *http.Request) {
	var req submitReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	pr := s.reviewStore.Submit(req.SessionID, req.AgentID, req.ToolName, req.ToolInput, req.RiskScore, req.Reason, req.RequestUID)
	telemetry.PendingReviewsGauge.Inc()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(submitReviewResponse{ID: pr.ID})
}

// waitReview handles GET /api/review/{id}/wait (unauthenticated — called by the hook process).
// Blocks until an operator decision arrives or the configured timeout elapses.
// Returns 200 {"decision":"ALLOW"} or 200 {"decision":"DENY"}.
func (s *APIServer) waitReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	timeout := 120 * time.Second

	approved, timedOut := s.reviewStore.Wait(id, timeout)
	telemetry.PendingReviewsGauge.Dec()

	decision := "DENY"
	if !timedOut && approved {
		decision = "ALLOW"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"decision": decision})
}

// reviewDecisionRequest is the optional body for approve/deny — names the operator
// acting on the review. Absent or unparsable body is treated as an anonymous decision.
type reviewDecisionRequest struct {
	ReviewedBy string `json:"reviewed_by,omitempty"`
}

// approveReview handles POST /api/review/{id}/approve (operator action, API-key guarded).
func (s *APIServer) approveReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pr, ok := s.reviewStore.Get(id)
	if !ok || !s.reviewStore.Decide(id, true) {
		http.Error(w, "review not found or already decided", http.StatusNotFound)
		return
	}
	var req reviewDecisionRequest
	json.NewDecoder(r.Body).Decode(&req)
	s.recordReviewOutcome(pr, "APPROVED", req.ReviewedBy)
	w.WriteHeader(http.StatusNoContent)
}

// denyReview handles POST /api/review/{id}/deny (operator action, API-key guarded).
func (s *APIServer) denyReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pr, ok := s.reviewStore.Get(id)
	if !ok || !s.reviewStore.Decide(id, false) {
		http.Error(w, "review not found or already decided", http.StatusNotFound)
		return
	}
	var req reviewDecisionRequest
	json.NewDecoder(r.Body).Decode(&req)
	s.recordReviewOutcome(pr, "DENIED", req.ReviewedBy)
	w.WriteHeader(http.StatusNoContent)
}

// recordReviewOutcome persists the operator's decision onto the originating audit
// record. A no-op when the pending review carries no RequestUID (submitter didn't
// supply one) or the durable decision store isn't configured.
func (s *APIServer) recordReviewOutcome(pr *review.PendingReview, status, reviewedBy string) {
	if s.decisionStore == nil || pr.RequestUID == "" {
		return
	}
	_ = s.decisionStore.MarkReviewed(pr.RequestUID, status, reviewedBy)
}

// listPendingReviews handles GET /api/review/pending (operator view, API-key guarded).
func (s *APIServer) listPendingReviews(w http.ResponseWriter, r *http.Request) {
	pending := s.reviewStore.ListPending()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pending); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
	}
}

// getReview handles GET /api/review/{id} (operator view, API-key guarded).
func (s *APIServer) getReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pr, ok := s.reviewStore.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pr)
}

// registerReviewRoutes mounts review endpoints onto mux.
// Hook endpoints are unauthenticated (hook process cannot carry API key).
// Operator endpoints are API-key guarded.
func (s *APIServer) registerReviewRoutes(mux *http.ServeMux) {
	guard := requireAPIKey(s.keyStore)

	// Unauthenticated — called by the hook subprocess.
	mux.HandleFunc("POST /api/review", s.submitReview)
	mux.HandleFunc("GET /api/review/{id}/wait", s.waitReview)

	// API-key guarded — operator actions.
	mux.Handle("GET /api/review/pending", guard(http.HandlerFunc(s.listPendingReviews)))
	mux.Handle("GET /api/review/{id}", guard(http.HandlerFunc(s.getReview)))
	mux.Handle("POST /api/review/{id}/approve", guard(http.HandlerFunc(s.approveReview)))
	mux.Handle("POST /api/review/{id}/deny", guard(http.HandlerFunc(s.denyReview)))
}
