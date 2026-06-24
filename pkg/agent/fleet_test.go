package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/pii"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

func testConfig() AgentFleetConfig {
	return AgentFleetConfig{
		DriftThreshold:     0.4,
		BehaviorWindowSize: 10,
		AttackChainEnabled: true,
	}
}

func testReq(sessionID, tool string) *admission.ToolCallAdmissionRequest {
	return &admission.ToolCallAdmissionRequest{
		UID:        "test-uid",
		SessionID:  sessionID,
		AgentID:    "test-agent",
		UserIntent: "refactor authentication middleware",
		ToolCall:   admission.ToolCall{Name: tool, Parameters: map[string]any{}},
		Timestamp:  time.Now(),
	}
}

func TestFleetManager_CleanSession(t *testing.T) {
	fm := NewFleetManager(testConfig(), store.NewMemoryStore())
	ctx := context.Background()

	// A single on-task read call should not trigger any violation.
	req := testReq("session-1", "read_file")
	if v := fm.Validate(ctx, req); v != nil {
		t.Fatalf("expected nil violation for clean call, got: %+v", v)
	}
}

func TestFleetManager_SessionIsolation(t *testing.T) {
	fm := NewFleetManager(testConfig(), store.NewMemoryStore())
	ctx := context.Background()

	// Drive session-A to a high risk state.
	for i := 0; i < 5; i++ {
		fm.Validate(ctx, testReq("session-A", "bash"))
	}

	// session-B should start clean with no inherited risk.
	req := testReq("session-B", "read_file")
	if v := fm.Validate(ctx, req); v != nil {
		t.Fatalf("session-B should not inherit risk from session-A, got: %+v", v)
	}
}

func TestFleetManager_AttackChainTriggersViolation(t *testing.T) {
	fm := NewFleetManager(testConfig(), store.NewMemoryStore())
	ctx := context.Background()

	calls := []string{"web_search", "bash", "http_post"}
	var lastViol *admission.PolicyViolation
	for _, tool := range calls {
		lastViol = fm.Validate(ctx, testReq("session-attack", tool))
	}
	// After a full recon→exploit→exfiltrate chain, a violation must have fired.
	if lastViol == nil {
		t.Fatal("expected a violation after full attack chain, got nil")
	}
	if lastViol.Layer != "agent_fleet" {
		t.Errorf("expected layer=agent_fleet, got %q", lastViol.Layer)
	}
}

func TestFleetManager_NameIdentifier(t *testing.T) {
	fm := NewFleetManager(testConfig(), store.NewMemoryStore())
	if fm.Name() != "agent_fleet" {
		t.Errorf("expected Name()=agent_fleet, got %q", fm.Name())
	}
}

func TestFleetManager_SeedFromManifest_RedactsIntentBaseline(t *testing.T) {
	matcher, err := pii.NewMatcher([]string{`\b\d{3}-\d{2}-\d{4}\b`}, nil, "")
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	cfg := testConfig()
	cfg.IntentRedactor = matcher

	st := store.NewMemoryStore()
	fm := NewFleetManager(cfg, st)

	intentText := "review applicant file, ssn 123-45-6789 on record"
	fm.SeedFromManifest("session-seed", intentText, nil, nil, "manifest-1", "v1", "")

	ss, ok := st.Get("session-seed")
	if !ok {
		t.Fatal("expected session state to be persisted")
	}
	if strings.Contains(ss.IntentBaseline, "123-45-6789") {
		t.Errorf("expected SSN to be redacted from IntentBaseline, got %q", ss.IntentBaseline)
	}
	if !strings.Contains(ss.IntentBaseline, "[REDACTED:PII]") {
		t.Errorf("expected redaction marker in IntentBaseline, got %q", ss.IntentBaseline)
	}
}

func TestFleetManager_SeedFromManifest_NilRedactorLeavesTextUnchanged(t *testing.T) {
	cfg := testConfig()
	cfg.IntentRedactor = nil

	st := store.NewMemoryStore()
	fm := NewFleetManager(cfg, st)

	intentText := "review applicant file, ssn 123-45-6789 on record"
	fm.SeedFromManifest("session-seed", intentText, nil, nil, "manifest-1", "v1", "")

	ss, ok := st.Get("session-seed")
	if !ok {
		t.Fatal("expected session state to be persisted")
	}
	if ss.IntentBaseline != intentText {
		t.Errorf("expected unchanged IntentBaseline, got %q", ss.IntentBaseline)
	}
}
