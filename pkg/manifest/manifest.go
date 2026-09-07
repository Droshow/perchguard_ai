// Package manifest defines the AgentManifest — the declared identity and mission
// of a governed agent. A manifest is submitted at session registration time and
// becomes the authoritative baseline for PerchGuard's intent enforcement.
package manifest

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
)

// AgentManifest is the declared identity, mission, and constraints of a governed agent.
type AgentManifest struct {
	APIVersion     string            `yaml:"apiVersion"        json:"apiVersion"`
	Kind           string            `yaml:"kind"              json:"kind"`
	Metadata       Metadata          `yaml:"metadata"          json:"metadata"`
	Mission        Mission           `yaml:"mission"           json:"mission"`
	Authorization  Authorization     `yaml:"authorization"     json:"authorization"`
	Invariants     []string          `yaml:"invariants"        json:"invariants"`
	ProjectContext ProjectContextRef `yaml:"project_context"   json:"project_context"`
	// ParentSessionID links this agent to its governing parent session.
	// When set, PerchGuard validates that this agent's tool scope is a subset of
	// the parent's and records the delegation chain for fleet visibility.
	ParentSessionID string `yaml:"parent_session_id" json:"parent_session_id,omitempty"`
}

// Metadata identifies the agent across registrations.
type Metadata struct {
	ID      string `yaml:"id"      json:"id"`
	Owner   string `yaml:"owner"   json:"owner"`
	Created string `yaml:"created" json:"created"`
	Version string `yaml:"version" json:"version"`
}

// Mission declares what the agent is authorized to do and what is out of scope.
type Mission struct {
	Summary    string   `yaml:"summary"      json:"summary"`
	Scope      []string `yaml:"scope"        json:"scope"`
	OutOfScope []string `yaml:"out_of_scope" json:"out_of_scope"`
	// Phases are additional declared task phases for long-running agents. Each
	// phase seeds its own intent-drift baseline (see pkg/agent.IntentModel), so a
	// call that's on-task for a later phase doesn't false-positive against the
	// registration-time baseline alone. Capped at 4 — combined with the fused
	// Summary+Scope+PRD baseline, this bounds total baselines at 5.
	Phases []string `yaml:"phases,omitempty" json:"phases,omitempty"`
}

// Authorization maps the agent to existing PerchGuard role-based policies.
type Authorization struct {
	Role                   string   `yaml:"role"                      json:"role"`
	AllowedSystems         []string `yaml:"allowed_systems"           json:"allowed_systems"`
	HumanReviewRequiredFor []string `yaml:"human_review_required_for" json:"human_review_required_for"`
}

// ProjectContextRef links the manifest to a project context store for intent enrichment.
type ProjectContextRef struct {
	ProjectID  string   `yaml:"project_id"  json:"project_id"`
	PRDVersion string   `yaml:"prd_version" json:"prd_version"`
	ADRRefs    []string `yaml:"adr_refs"    json:"adr_refs"`
}

// Parse decodes an AgentManifest from raw bytes.
// Tries JSON when the payload starts with '{', otherwise YAML.
func Parse(data []byte) (*AgentManifest, error) {
	var m AgentManifest
	if strings.TrimSpace(string(data))[:1] == "{" {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("json parse: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("yaml parse: %w", err)
		}
	}
	if err := validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func validate(m *AgentManifest) error {
	if m.Metadata.ID == "" {
		return fmt.Errorf("manifest.metadata.id is required")
	}
	if m.Metadata.Version == "" {
		return fmt.Errorf("manifest.metadata.version is required")
	}
	if m.Mission.Summary == "" {
		return fmt.Errorf("manifest.mission.summary is required")
	}
	if len(m.Mission.Phases) > 4 {
		return fmt.Errorf("manifest.mission.phases: max 4 phases")
	}
	return nil
}

// IntentText returns the combined text used to seed a SessionAgent's intent baseline.
// Mission summary is the anchor; in-scope declarations add vocabulary.
func (m *AgentManifest) IntentText() string {
	parts := []string{m.Mission.Summary}
	parts = append(parts, m.Mission.Scope...)
	return strings.Join(parts, " ")
}
