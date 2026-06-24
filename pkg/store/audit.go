package store

import (
	"sync"
	"time"
)

// AuditRecord is a store-layer snapshot of an admission decision.
// Uses string Decision to avoid importing pkg/admission (import cycle prevention).
type AuditRecord struct {
	RequestUID    string    `json:"request_uid"`
	SessionID     string    `json:"session_id"`
	AgentID       string    `json:"agent_id"`
	ToolName      string    `json:"tool_name"`
	Decision      string    `json:"decision"` // "ALLOW" | "DENY" | "MUTATE" | "HUMAN_REVIEW" | "TERMINATE"
	PolicyHit     string    `json:"policy_hit,omitempty"`
	Reason        string    `json:"reason"`
	Timestamp     time.Time `json:"timestamp"`
	DurationMs    int64     `json:"duration_ms"`
	RiskScore     float64  `json:"risk_score"`     // cumulative session risk at the moment of this decision
	Registered    bool     `json:"registered"`     // false when no valid agent token was presented
	PolicyVersion string   `json:"policy_version"` // sha256[:8] of policies.yaml at decision time
	DataRefsIn    []string `json:"data_refs_in,omitempty"`
	DataRefOut    string   `json:"data_ref_out,omitempty"`
	DriftScore    *float64 `json:"drift_score,omitempty"` // intent drift for this call; nil when no fleet validator scored it

	// HITL review outcome, populated by SQLiteAuditSink.MarkReviewed after the fact.
	// Empty/nil until an operator acts on a HUMAN_REVIEW decision.
	ApprovalStatus string     `json:"approval_status,omitempty"`
	ReviewedBy     string     `json:"reviewed_by,omitempty"`
	ReviewedAt     *time.Time `json:"reviewed_at,omitempty"`
}

// AuditFilter scopes a Query call. Zero values mean "no filter".
type AuditFilter struct {
	Decision  string // empty = any
	ToolName  string // empty = any
	SessionID string // empty = any
	Limit     int    // 0 = default (100); capped at 1000
}

// AuditRingBuffer is a thread-safe circular buffer of AuditRecord entries.
// Oldest entries are overwritten when the buffer is full.
type AuditRingBuffer struct {
	mu       sync.RWMutex
	buf      []AuditRecord
	head     int // next write position
	count    int // total records stored (≤ capacity)
	capacity int
	onPush   func(AuditRecord) // optional hook, called after each Push with the lock released
}

// SetPushHook registers a function called after every Push.
// Used to tee records to the JSONL forensic sink without blocking the admission pipeline.
func (r *AuditRingBuffer) SetPushHook(fn func(AuditRecord)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onPush = fn
}

// PushHook returns the currently registered push hook, or nil.
func (r *AuditRingBuffer) PushHook() func(AuditRecord) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.onPush
}

func NewAuditRingBuffer(capacity int) *AuditRingBuffer {
	return &AuditRingBuffer{
		buf:      make([]AuditRecord, capacity),
		capacity: capacity,
	}
}

// Push appends a record, overwriting the oldest entry when the buffer is full.
func (r *AuditRingBuffer) Push(rec AuditRecord) {
	r.mu.Lock()
	r.buf[r.head] = rec
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
	hook := r.onPush
	r.mu.Unlock()
	if hook != nil {
		hook(rec)
	}
}

// Len returns the number of records currently stored in the buffer.
func (r *AuditRingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.count
}

// Query returns records matching f, newest-first, up to f.Limit.
func (r *AuditRingBuffer) Query(f AuditFilter) []AuditRecord {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make([]AuditRecord, 0, limit)
	for i := 0; i < r.count && len(results) < limit; i++ {
		idx := (r.head - 1 - i + r.capacity) % r.capacity
		rec := r.buf[idx]
		if f.Decision != "" && rec.Decision != f.Decision {
			continue
		}
		if f.ToolName != "" && rec.ToolName != f.ToolName {
			continue
		}
		if f.SessionID != "" && rec.SessionID != f.SessionID {
			continue
		}
		results = append(results, rec)
	}
	return results
}
