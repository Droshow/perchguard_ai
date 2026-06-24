package audit_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// ── JSONLRecordSink ───────────────────────────────────────────────────────────

func TestJSONLRecordSink_WritesValidLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	sink, err := audit.NewJSONLRecordSink(path)
	if err != nil {
		t.Fatalf("NewJSONLRecordSink: %v", err)
	}

	rec := store.AuditRecord{
		RequestUID: "req-1",
		SessionID:  "s1",
		ToolName:   "read_file",
		Decision:   "DENY",
		Timestamp:  time.Now(),
	}
	sink.Push(rec)

	// Push is async; give it time to write.
	time.Sleep(50 * time.Millisecond)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading sink file: %v", err)
	}
	if !strings.Contains(string(data), "DENY") {
		t.Errorf("expected DENY in sink output, got: %s", string(data))
	}

	// Must be valid JSON.
	var got store.AuditRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &got); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}
	if got.SessionID != "s1" {
		t.Errorf("want session_id s1, got %s", got.SessionID)
	}
}

func TestJSONLRecordSink_AppendsMultipleLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	sink, err := audit.NewJSONLRecordSink(path)
	if err != nil {
		t.Fatalf("NewJSONLRecordSink: %v", err)
	}

	sink.Push(store.AuditRecord{RequestUID: "r1", Decision: "ALLOW"})
	sink.Push(store.AuditRecord{RequestUID: "r2", Decision: "DENY"})

	time.Sleep(100 * time.Millisecond)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
}

func TestJSONLRecordSink_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "audit.jsonl")

	_, err := audit.NewJSONLRecordSink(path)
	if err != nil {
		t.Fatalf("should create parent dirs: %v", err)
	}
}

// ── BuildRecord ───────────────────────────────────────────────────────────────

func TestBuildRecord_ToolCallCount(t *testing.T) {
	decisions := map[string]int{"ALLOW": 3, "DENY": 2}
	rec := audit.BuildRecord("agent-1", "v1", "sess-1", "", nil,
		time.Now(), time.Now(),
		nil, nil, decisions, 0.5, false, "", nil, nil, 0,
	)

	if rec.SessionContext.ToolCalls != 5 {
		t.Errorf("want 5 tool calls, got %d", rec.SessionContext.ToolCalls)
	}
}

func TestBuildRecord_IDFormat(t *testing.T) {
	ended := time.Date(2026, 5, 7, 14, 30, 0, 0, time.UTC)
	rec := audit.BuildRecord("a", "v1", "s", "", nil, time.Now(), ended, nil, nil, nil, 0, false, "", nil, nil, 0)

	if !strings.HasPrefix(rec.ID, "snap-") {
		t.Errorf("ID should start with snap-, got %s", rec.ID)
	}
	if !strings.Contains(rec.ID, "20260507") {
		t.Errorf("ID should contain date 20260507, got %s", rec.ID)
	}
}

func TestBuildRecord_Source(t *testing.T) {
	rec := audit.BuildRecord("a", "v1", "s", "", nil, time.Now(), time.Now(), nil, nil, nil, 0, false, "", nil, nil, 0)
	if rec.Source != "perchguard" {
		t.Errorf("want source=perchguard, got %s", rec.Source)
	}
}

func TestBuildRecord_TerminatedEarly(t *testing.T) {
	rec := audit.BuildRecord("a", "v1", "s", "", nil, time.Now(), time.Now(), nil, nil, nil, 0.8, true, "", nil, nil, 0)
	if !rec.SessionContext.TerminatedEarly {
		t.Error("want TerminatedEarly=true")
	}
	if rec.SessionContext.PeakRiskScore != 0.8 {
		t.Errorf("want peak risk 0.8, got %.2f", rec.SessionContext.PeakRiskScore)
	}
}

// ── NoOpContextProvider ───────────────────────────────────────────────────────

func TestNoOpContextProvider(t *testing.T) {
	var p audit.NoOpContextProvider
	ctx := p.ReadContext(5)
	if ctx.Loaded {
		t.Error("NoOpContextProvider should return Loaded=false")
	}
	if ctx.IntentText() != "" {
		t.Error("NoOpContextProvider should return empty IntentText")
	}
}
