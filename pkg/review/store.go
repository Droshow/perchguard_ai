// Package review manages the in-process human-in-the-loop approval flow.
//
// When the admission pipeline returns a REVIEW decision, the claude-hook
// submits the pending call here via POST /api/review and blocks on Wait.
// The operator approves or denies via the dashboard or Slack; Decide unblocks
// the waiting hook. If the operator does not respond within the timeout, Wait
// returns (false, timeout=true) and the hook denies.
package review

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Status is the lifecycle state of a pending review.
type Status string

const (
	StatusPending  Status = "PENDING"
	StatusApproved Status = "APPROVED"
	StatusDenied   Status = "DENIED"
	StatusTimeout  Status = "TIMEOUT"
)

// PendingReview holds a single tool call awaiting operator decision.
type PendingReview struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	AgentID   string         `json:"agent_id"`
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	RiskScore float64        `json:"risk_score"`
	Reason    string         `json:"reason"`
	CreatedAt time.Time      `json:"created_at"`

	// RequestUID is the originating admission decision's audit RequestUID, when the
	// submitter has one. Lets the operator's approve/deny action be written back onto
	// that audit record via SQLiteAuditSink.MarkReviewed. Empty when not supplied.
	RequestUID string `json:"request_uid,omitempty"`

	decideCh chan bool // closed/sent by Decide
}

// Store is the in-memory registry of all pending reviews.
type Store struct {
	mu      sync.Mutex
	pending map[string]*PendingReview
}

func NewStore() *Store {
	return &Store{pending: make(map[string]*PendingReview)}
}

// Submit registers a new pending review and returns it.
// The returned PendingReview.ID is what the hook should pass to Wait.
func (s *Store) Submit(sessionID, agentID, toolName string, toolInput map[string]any, riskScore float64, reason, requestUID string) *PendingReview {
	pr := &PendingReview{
		ID:         newID(),
		SessionID:  sessionID,
		AgentID:    agentID,
		ToolName:   toolName,
		ToolInput:  toolInput,
		RiskScore:  riskScore,
		Reason:     reason,
		CreatedAt:  time.Now(),
		RequestUID: requestUID,
		decideCh:   make(chan bool, 1),
	}
	s.mu.Lock()
	s.pending[pr.ID] = pr
	s.mu.Unlock()
	return pr
}

// Wait blocks until a decision is posted or the timeout elapses.
// Returns (approved, timedOut). On timeout, the pending review is removed.
func (s *Store) Wait(id string, timeout time.Duration) (approved bool, timedOut bool) {
	s.mu.Lock()
	pr, ok := s.pending[id]
	s.mu.Unlock()
	if !ok {
		return false, false
	}

	select {
	case approved = <-pr.decideCh:
		s.remove(id)
		return approved, false
	case <-time.After(timeout):
		s.remove(id)
		return false, true
	}
}

// Decide sends an approval or denial to the waiting hook.
// Returns false when no pending review with that ID exists.
func (s *Store) Decide(id string, approved bool) bool {
	s.mu.Lock()
	pr, ok := s.pending[id]
	s.mu.Unlock()
	if !ok {
		return false
	}
	pr.decideCh <- approved
	return true
}

// ListPending returns all reviews currently awaiting a decision.
func (s *Store) ListPending() []*PendingReview {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*PendingReview, 0, len(s.pending))
	for _, pr := range s.pending {
		out = append(out, pr)
	}
	return out
}

// Get returns a single pending review by ID.
func (s *Store) Get(id string) (*PendingReview, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr, ok := s.pending[id]
	return pr, ok
}

func (s *Store) remove(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "rv-" + hex.EncodeToString(b)
}
