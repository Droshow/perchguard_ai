// Package store provides session state persistence for the PerchGuard agent fleet.
// The MemoryStore is the dev-mode implementation; production would swap in Redis.
package store

import (
	"sync"
	"time"
)

// ToolEvent is a single recorded tool invocation with its classified attack stage.
type ToolEvent struct {
	Tool       string
	Stage      string // "recon" | "exploit" | "escalate" | "exfiltrate" | "unknown"
	Timestamp  time.Time
	DriftScore float64 // semantic drift from session baseline at call time; used for velocity anomaly detection
}

// SessionState holds the mutable runtime state for one agent session.
type SessionState struct {
	SessionID       string
	ManifestID      string  // set when registered via POST /agents/register
	ManifestVersion string  // manifest version at registration time
	IntentBaseline   string   // fused manifest+context text the intent baseline was set from; written once at registration, never mutated
	IntentPhases     []string // manifest-declared Mission.Phases that seeded additional drift baselines; written once at registration
	IntentOutOfScope []string // manifest-declared Mission.OutOfScope vocabulary; written once at registration
	ParentSessionID    string  // non-empty when this is a delegated sub-agent session
	DelegatedCallLimit int     // >0 overrides policy MaxToolCallsPerSession for this child session
	RiskScore       float64
	PeakRiskScore   float64 // highest risk score observed across the session lifetime
	Events          []ToolEvent
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// SessionStore is the minimal persistence interface the fleet uses.
// Swap MemoryStore for a Redis-backed implementation without touching any agent code.
type SessionStore interface {
	Get(sessionID string) (*SessionState, bool)
	Set(sessionID string, state *SessionState)
	Delete(sessionID string)
	List() []*SessionState
	// Children returns the session IDs of all direct sub-agents of parentID.
	Children(parentID string) []string
}

// MemoryStore is a concurrent in-memory SessionStore for local development.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: make(map[string]*SessionState)}
}

func (m *MemoryStore) Get(sessionID string) (*SessionState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

func (m *MemoryStore) Set(sessionID string, state *SessionState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = state
}

func (m *MemoryStore) Delete(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

func (m *MemoryStore) List() []*SessionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SessionState, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

func (m *MemoryStore) Children(parentID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	for id, s := range m.sessions {
		if s.ParentSessionID == parentID {
			out = append(out, id)
		}
	}
	return out
}
