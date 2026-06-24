package localfs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
)

// buildFakeContextRoot creates a temp directory with the project context structure.
func buildFakeContextRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	must(t, os.MkdirAll(filepath.Join(root, contextDir, snapshotsDir), 0755))
	must(t, os.MkdirAll(filepath.Join(root, contextDir, prdsDir), 0755))

	prd := "# Test PRD\n\nThis agent reviews insurance policy documents for compliance.\n"
	must(t, os.WriteFile(filepath.Join(root, contextDir, prdsDir, "20260505-120000-test.md"), []byte(prd), 0644))

	entries := []map[string]any{
		{"id": "snap-1", "summary": "Implemented claims query module.", "source": "mcp", "timestamp": "2026-05-04T10:00:00Z"},
		{"id": "snap-2", "summary": "This is a perchguard snapshot.", "source": "perchguard", "timestamp": "2026-05-04T11:00:00Z"},
		{"id": "snap-3", "summary": "Added output validation for compliance reports.", "source": "cli", "timestamp": "2026-05-05T09:00:00Z"},
	}
	data, _ := json.MarshalIndent(entries, "", "  ")
	must(t, os.WriteFile(filepath.Join(root, contextDir, snapshotsDir, entriesFile), data, 0644))

	return root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadContextLoaded(t *testing.T) {
	root := buildFakeContextRoot(t)
	r := NewReader(root)

	ctx := r.ReadContext(5)
	if !ctx.Loaded {
		t.Fatal("expected Loaded=true")
	}
	if !strings.Contains(ctx.PRDSummary, "insurance policy") {
		t.Errorf("PRDSummary missing expected content: %q", ctx.PRDSummary)
	}
}

func TestReadContextFiltersPerchguardSnapshots(t *testing.T) {
	root := buildFakeContextRoot(t)
	r := NewReader(root)
	ctx := r.ReadContext(10)

	for _, s := range ctx.RecentSnapshots {
		if s.ID == "snap-2" {
			t.Error("perchguard-emitted snapshot should be filtered out")
		}
	}
	if len(ctx.RecentSnapshots) != 2 {
		t.Errorf("expected 2 human snapshots, got %d", len(ctx.RecentSnapshots))
	}
}

func TestReadContextLimit(t *testing.T) {
	root := buildFakeContextRoot(t)
	r := NewReader(root)
	ctx := r.ReadContext(1)
	if len(ctx.RecentSnapshots) != 1 {
		t.Errorf("expected 1 snapshot with limit=1, got %d", len(ctx.RecentSnapshots))
	}
}

func TestReadContextNoRoot(t *testing.T) {
	r := NewReader("/nonexistent/path/xyz")
	ctx := r.ReadContext(5)
	if ctx.Loaded {
		t.Error("expected Loaded=false for nonexistent root")
	}
	if ctx.IntentText() != "" {
		t.Error("IntentText should be empty when not loaded")
	}
}

func TestIntentTextCombines(t *testing.T) {
	root := buildFakeContextRoot(t)
	r := NewReader(root)
	ctx := r.ReadContext(5)
	text := ctx.IntentText()
	if !strings.Contains(text, "insurance policy") {
		t.Errorf("IntentText missing PRD content: %q", text)
	}
	if !strings.Contains(text, "claims query") {
		t.Errorf("IntentText missing snapshot content: %q", text)
	}
}

func TestWriteGovernanceSnapshot(t *testing.T) {
	root := buildFakeContextRoot(t)
	w := NewWriter(root)

	rec := audit.BuildRecord(
		"finbridge-compliance-agent-v2",
		"2.3.1",
		"pg-finbridge-abc123",
		"", nil,
		time.Now().Add(-5*time.Minute),
		time.Now(),
		[]audit.ToolCallStep{
			{Seq: 1, Tool: "read_report", Decision: "ALLOW", RiskScore: 0.1},
			{Seq: 2, Tool: "database:drop", Decision: "DENY", RiskScore: 0.42, Reason: "policy violation"},
		},
		[]audit.GovernanceEvent{
			{Timestamp: time.Now().Format(time.RFC3339), Decision: "DENY", Tool: "database:drop", Reason: "policy violation"},
		},
		map[string]int{"ALLOW": 10, "DENY": 1},
		0.42,
		false,
		"",
		nil,
		nil,
		0,
	)

	if err := w.WriteGovernanceSnapshot(rec); err != nil {
		t.Fatalf("WriteGovernanceSnapshot: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, contextDir, snapshotsDir, entriesFile))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}

	last := entries[len(entries)-1]
	if last["source"] != "perchguard" {
		t.Errorf("source: got %v", last["source"])
	}
	if !strings.Contains(last["summary"].(string), "finbridge-compliance-agent-v2") {
		t.Errorf("summary: got %q", last["summary"])
	}

	ctx, ok := last["session_context"].(map[string]any)
	if !ok {
		t.Fatal("session_context missing or wrong type")
	}
	if ctx["agent_id"] != "finbridge-compliance-agent-v2" {
		t.Errorf("agent_id: got %v", ctx["agent_id"])
	}
}

func TestWriteGovernanceSnapshotNoRoot(t *testing.T) {
	w := NewWriter("/nonexistent/path")
	rec := audit.BuildRecord("a", "1", "s", "", nil, time.Now(), time.Now(), nil, nil, nil, 0, false, "", nil, nil, 0)
	if err := w.WriteGovernanceSnapshot(rec); err == nil {
		t.Error("expected error when root not found")
	}
}

func TestBuildRecordTotals(t *testing.T) {
	decisions := map[string]int{"ALLOW": 44, "DENY": 2, "MUTATE": 1}
	rec := audit.BuildRecord("agent", "1.0", "sess", "", nil, time.Now(), time.Now(), nil, nil, decisions, 0.5, true, "", nil, nil, 0)
	if rec.SessionContext.ToolCalls != 47 {
		t.Errorf("ToolCalls: got %d", rec.SessionContext.ToolCalls)
	}
	if !rec.SessionContext.TerminatedEarly {
		t.Error("TerminatedEarly should be true")
	}
	if rec.Source != "perchguard" {
		t.Errorf("Source: got %q", rec.Source)
	}
}

func TestFindRoot(t *testing.T) {
	root := buildFakeContextRoot(t)
	subdir := filepath.Join(root, "subdir")
	must(t, os.MkdirAll(subdir, 0755))
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(subdir)

	found := FindRoot()
	if found != root {
		t.Errorf("FindRoot: got %q, want %q", found, root)
	}
}
