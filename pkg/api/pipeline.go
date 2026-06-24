package api

import (
	"net/http"
	"time"
)

// PipelineStatus describes the currently active admission pipeline.
type PipelineStatus struct {
	Validators       []string  `json:"validators"`
	Mutators         []string  `json:"mutators"`
	Quotas           []string  `json:"quotas"`
	SemanticFirewall bool      `json:"semantic_firewall_enabled"`
	EnforcementMode  string    `json:"enforcement_mode"` // "observe" or "enforce"
	PolicyHash       string    `json:"policy_hash"`
	PolicyPath       string    `json:"policy_path"`
	LoadedAt         time.Time `json:"loaded_at"`
}

func (s *APIServer) getPipeline(w http.ResponseWriter, r *http.Request) {
	meta := s.policyMeta.Load()

	sfEnabled := false
	if meta != nil && meta.Config != nil {
		sfEnabled = meta.Config.Policies.SemanticFirewall.Enabled
	}

	enforceMode := "enforce"
	if s.interceptor.ObserveMode() {
		enforceMode = "observe"
	}

	status := PipelineStatus{
		Validators:       s.interceptor.ValidatorNames(),
		Mutators:         s.interceptor.MutatorNames(),
		Quotas:           s.interceptor.QuotaNames(),
		SemanticFirewall: sfEnabled,
		EnforcementMode:  enforceMode,
	}
	if meta != nil {
		status.PolicyHash = meta.Hash
		status.PolicyPath = meta.Path
		status.LoadedAt = meta.LoadedAt
	}
	writeJSON(w, http.StatusOK, status)
}
