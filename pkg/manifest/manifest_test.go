package manifest

import (
	"strings"
	"testing"
)

const exampleYAML = `
apiVersion: regentics.ai/v1
kind: AgentManifest
metadata:
  id: "finbridge-compliance-agent-v2"
  owner: "platform-team@finbridge.com"
  created: "2026-04-01"
  version: "2.3.1"
mission:
  summary: "Review insurance policy documents for renewal compliance."
  scope:
    - "Read policy documents in /workspace/policies/"
    - "Query the claims database for the current session's policy"
  out_of_scope:
    - "Any write operation to the claims database"
authorization:
  role: "developer_agent"
  allowed_systems:
    - "claims-db.internal"
  human_review_required_for:
    - "write_report"
invariants:
  - "never_exfiltrate_pii: tool_output must not contain SSN"
project_context:
  project_id: "finbridge-compliance"
  prd_version: "1.4"
  adr_refs:
    - "ADR-007: database read-only access"
`

const exampleJSON = `{
  "apiVersion": "regentics.ai/v1",
  "kind": "AgentManifest",
  "metadata": {"id": "test-agent", "version": "1.0.0", "owner": "ops@test.com", "created": "2026-05-05"},
  "mission": {"summary": "Run integration tests in CI.", "scope": ["bash:pytest"], "out_of_scope": []},
  "authorization": {"role": "developer_agent", "allowed_systems": [], "human_review_required_for": []},
  "invariants": [],
  "project_context": {}
}`

func TestParseYAML(t *testing.T) {
	m, err := Parse([]byte(exampleYAML))
	if err != nil {
		t.Fatalf("Parse YAML: %v", err)
	}
	if m.Metadata.ID != "finbridge-compliance-agent-v2" {
		t.Errorf("id: got %q", m.Metadata.ID)
	}
	if m.Metadata.Version != "2.3.1" {
		t.Errorf("version: got %q", m.Metadata.Version)
	}
	if m.Mission.Summary == "" {
		t.Error("mission.summary empty")
	}
	if m.Authorization.Role != "developer_agent" {
		t.Errorf("role: got %q", m.Authorization.Role)
	}
	if len(m.Mission.Scope) != 2 {
		t.Errorf("scope length: got %d", len(m.Mission.Scope))
	}
	if len(m.Invariants) != 1 {
		t.Errorf("invariants length: got %d", len(m.Invariants))
	}
	if m.ProjectContext.ProjectID != "finbridge-compliance" {
		t.Errorf("project_context.project_id: got %q", m.ProjectContext.ProjectID)
	}
}

func TestParseJSON(t *testing.T) {
	m, err := Parse([]byte(exampleJSON))
	if err != nil {
		t.Fatalf("Parse JSON: %v", err)
	}
	if m.Metadata.ID != "test-agent" {
		t.Errorf("id: got %q", m.Metadata.ID)
	}
}

func TestParseMissingID(t *testing.T) {
	bad := `
metadata:
  version: "1.0"
mission:
  summary: "do stuff"
`
	_, err := Parse([]byte(bad))
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestParseMissingSummary(t *testing.T) {
	bad := `
metadata:
  id: "x"
  version: "1.0"
mission:
  summary: ""
`
	_, err := Parse([]byte(bad))
	if err == nil {
		t.Fatal("expected error for empty mission summary")
	}
}

func TestIntentText(t *testing.T) {
	m, _ := Parse([]byte(exampleYAML))
	text := m.IntentText()
	if !strings.Contains(text, "insurance policy") {
		t.Errorf("intent text missing mission summary content: %q", text)
	}
	if !strings.Contains(text, "/workspace/policies") {
		t.Errorf("intent text missing scope content: %q", text)
	}
}

func TestParsePhasesCap(t *testing.T) {
	fourPhases := `
metadata:
  id: "x"
  version: "1.0"
mission:
  summary: "do stuff"
  phases:
    - "phase 1"
    - "phase 2"
    - "phase 3"
    - "phase 4"
`
	if _, err := Parse([]byte(fourPhases)); err != nil {
		t.Fatalf("expected 4 phases to be accepted, got %v", err)
	}

	fivePhases := `
metadata:
  id: "x"
  version: "1.0"
mission:
  summary: "do stuff"
  phases:
    - "phase 1"
    - "phase 2"
    - "phase 3"
    - "phase 4"
    - "phase 5"
`
	if _, err := Parse([]byte(fivePhases)); err == nil {
		t.Fatal("expected error for more than 4 phases")
	}
}

func TestStoreRegisterAndVerify(t *testing.T) {
	s := NewStore()
	m, _ := Parse([]byte(exampleYAML))

	tok, err := IssueToken()
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if !strings.HasPrefix(tok, "pgat-") {
		t.Errorf("token format: got %q", tok)
	}

	sid, err := NewSessionID(m.Metadata.ID)
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	if !strings.HasPrefix(sid, "pg-") {
		t.Errorf("session id format: got %q", sid)
	}

	reg := &Registration{Manifest: m, SessionID: sid, Token: tok}
	s.Register(reg)

	got, ok := s.GetBySession(sid)
	if !ok {
		t.Fatal("GetBySession: not found")
	}
	if got.Manifest.Metadata.ID != m.Metadata.ID {
		t.Error("wrong manifest returned")
	}

	if !s.Verify(sid, m.Metadata.ID, tok) {
		t.Error("Verify should return true for correct token")
	}
	if s.Verify(sid, m.Metadata.ID, "wrong-token") {
		t.Error("Verify should return false for wrong token")
	}
	if s.Verify("bad-session", m.Metadata.ID, tok) {
		t.Error("Verify should return false for unknown session")
	}
}

func TestStoreDelete(t *testing.T) {
	s := NewStore()
	m, _ := Parse([]byte(exampleYAML))
	tok, _ := IssueToken()
	sid, _ := NewSessionID(m.Metadata.ID)
	s.Register(&Registration{Manifest: m, SessionID: sid, Token: tok})

	s.Delete(sid)
	if _, ok := s.GetBySession(sid); ok {
		t.Error("expected registration to be deleted")
	}
	if s.Verify(sid, m.Metadata.ID, tok) {
		t.Error("expected Verify to fail after deletion")
	}
}
