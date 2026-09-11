package api

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/agent"
	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
)

type registrationResponse struct {
	AgentID           string   `json:"agent_id"`
	ManifestVersion   string   `json:"manifest_version"`
	SessionID         string   `json:"session_id"`
	Token             string   `json:"token"`
	EffectivePolicies []string `json:"effective_policies"`
	ContextLoaded     bool     `json:"context_loaded"`
	ParentSessionID   string   `json:"parent_session_id,omitempty"`
}

// registerAgent handles POST /agents/register.
//
// The caller submits an AgentManifest (YAML or JSON). PerchGuard:
//  1. Parses and validates the manifest.
//  2. Reads context from the configured ContextProvider to enrich the intent baseline.
//  3. Pre-seeds the SessionAgent so drift detection is accurate from call one.
//  4. Issues a session token the agent must present on subsequent /intercept calls.
//
// The response is always JSON. A 400 is returned when the manifest is invalid.
func (s *APIServer) registerAgent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	m, err := manifest.Parse(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid manifest: "+err.Error())
		return
	}

	sessionID, err := manifest.NewSessionID(m.Metadata.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session id generation failed")
		return
	}

	token, err := manifest.IssueToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token issuance failed")
		return
	}

	// Delegation: if parent_session_id is set, the caller must prove possession of
	// that session's own token before we record it as a parent — otherwise
	// parent_session_id is just a claim, and a delegation chain built on unverified
	// claims (e.g. an orchestration layer relaying ids through mutable shared state)
	// lets a registration assert any lineage it likes. Verify() is the same
	// constant-time check /intercept already applies to this header.
	if m.ParentSessionID != "" {
		if s.manifestStore == nil {
			writeError(w, http.StatusBadRequest, "delegation requires manifest store")
			return
		}
		parentReg, ok := s.manifestStore.GetBySession(m.ParentSessionID)
		if !ok {
			writeError(w, http.StatusBadRequest, "parent_session_id not found or expired")
			return
		}
		parentToken := r.Header.Get("X-PerchGuard-Agent-Token")
		if parentToken == "" || !s.manifestStore.Verify(m.ParentSessionID, parentReg.Manifest.Metadata.ID, parentToken) {
			writeError(w, http.StatusForbidden, "parent session token verification failed")
			return
		}
		if lr := s.policyMeta.Load(); lr != nil {
			if err := agent.ValidateScope(m, parentReg.Manifest, lr.Config); err != nil {
				writeError(w, http.StatusBadRequest, "delegation scope violation: "+err.Error())
				return
			}
		}
		if s.delegationStore != nil {
			s.delegationStore.Record(sessionID, m.ParentSessionID)
		}
	}

	// Pre-seed the session agent with the declared intent before the first tool call.
	// Done here so SeedFromManifest creates the SessionState before we set DelegatedCallLimit.
	intentText := m.IntentText()
	ctx := s.contextProvider.ReadContext(3)
	if ctx.Loaded {
		if extra := ctx.IntentText(); extra != "" {
			intentText = strings.TrimSpace(intentText + " " + extra)
		}
	}
	if s.fleet != nil {
		s.fleet.SeedFromManifest(sessionID, intentText, m.Mission.Phases, m.Mission.OutOfScope, m.Metadata.ID, m.Metadata.Version, m.ParentSessionID)
	}

	// Budget inheritance: compute delegated call limit from policy fraction and write to SessionState.
	if m.ParentSessionID != "" {
		if lr := s.policyMeta.Load(); lr != nil {
			fraction := lr.Config.Policies.SessionBudget.DelegationFraction
			if fraction > 0 {
				original := lr.Config.Policies.SessionBudget.Limits.MaxToolCallsPerSession
				delegated := int(float64(original) * fraction)
				if delegated < 1 {
					delegated = 1
				}
				if ss, ok := s.sessions.Get(sessionID); ok {
					ss.DelegatedCallLimit = delegated
					s.sessions.Set(sessionID, ss)
				}
			}
		}
	}

	// Register in the manifest store for token verification on /intercept.
	if s.manifestStore != nil {
		s.manifestStore.Register(&manifest.Registration{
			Manifest:     m,
			SessionID:    sessionID,
			Token:        token,
			RegisteredAt: time.Now(),
		})
	}

	writeJSON(w, http.StatusCreated, registrationResponse{
		AgentID:           m.Metadata.ID,
		ManifestVersion:   m.Metadata.Version,
		SessionID:         sessionID,
		Token:             token,
		EffectivePolicies: s.interceptor.ValidatorNames(),
		ContextLoaded:     ctx.Loaded,
		ParentSessionID:   m.ParentSessionID,
	})
}

// getAgent handles GET /agents/{id} — returns the current registration for an agent.
func (s *APIServer) getAgent(w http.ResponseWriter, r *http.Request) {
	if s.manifestStore == nil {
		writeError(w, http.StatusServiceUnavailable, "manifest store not configured")
		return
	}

	sessionID := r.PathValue("id")
	reg, ok := s.manifestStore.GetBySession(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "no registration found for session")
		return
	}

	type agentDetail struct {
		AgentID         string   `json:"agent_id"`
		ManifestVersion string   `json:"manifest_version"`
		SessionID       string   `json:"session_id"`
		RegisteredAt    string   `json:"registered_at"`
		MissionSummary  string   `json:"mission_summary"`
		Scope           []string `json:"scope"`
		AuthRole        string   `json:"auth_role"`
	}
	writeJSON(w, http.StatusOK, agentDetail{
		AgentID:         reg.Manifest.Metadata.ID,
		ManifestVersion: reg.Manifest.Metadata.Version,
		SessionID:       reg.SessionID,
		RegisteredAt:    reg.RegisteredAt.UTC().Format(time.RFC3339),
		MissionSummary:  reg.Manifest.Mission.Summary,
		Scope:           reg.Manifest.Mission.Scope,
		AuthRole:        reg.Manifest.Authorization.Role,
	})
}
