package manifest

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Registration records a manifest and its associated session binding.
type Registration struct {
	Manifest     *AgentManifest
	SessionID    string
	Token        string
	RegisteredAt time.Time
}

// Store is the in-memory manifest registry, keyed by session_id (primary)
// and agent_id (secondary — latest registration per agent).
type Store struct {
	mu        sync.RWMutex
	bySession map[string]*Registration
	byAgent   map[string]*Registration
}

// NewStore creates an empty manifest Store.
func NewStore() *Store {
	return &Store{
		bySession: make(map[string]*Registration),
		byAgent:   make(map[string]*Registration),
	}
}

// Register stores a registration. A prior registration for the same agent is overwritten.
func (s *Store) Register(reg *Registration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bySession[reg.SessionID] = reg
	s.byAgent[reg.Manifest.Metadata.ID] = reg
}

// GetBySession returns the registration for a session_id.
func (s *Store) GetBySession(sessionID string) (*Registration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.bySession[sessionID]
	return r, ok
}

// Delete removes the registration for a session_id and cleans up the agent index.
func (s *Store) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.bySession[sessionID]
	if !ok {
		return
	}
	if other, ok2 := s.byAgent[r.Manifest.Metadata.ID]; ok2 && other.SessionID == sessionID {
		delete(s.byAgent, r.Manifest.Metadata.ID)
	}
	delete(s.bySession, sessionID)
}

// Verify returns true when token matches the stored token for (sessionID, agentID).
// Token comparison uses subtle.ConstantTimeCompare to prevent timing attacks.
func (s *Store) Verify(sessionID, agentID, token string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.bySession[sessionID]
	if !ok {
		return false
	}
	if r.Manifest.Metadata.ID != agentID {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(r.Token)) == 1
}

// IssueToken generates a cryptographically random session token.
// Format: pgat-<32 hex chars>
func IssueToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("token generation: %w", err)
	}
	return "pgat-" + hex.EncodeToString(b), nil
}

// NewSessionID generates a session ID bound to the registered agent.
// Format: pg-<agentID-prefix>-<12 hex chars>
func NewSessionID(agentID string) (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("session id generation: %w", err)
	}
	prefix := agentID
	if len(prefix) > 20 {
		prefix = prefix[:20]
	}
	return fmt.Sprintf("pg-%s-%s", prefix, hex.EncodeToString(b)), nil
}
