package agent

import (
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// TestClassifyTool_AlwaysUnknown verifies the contract: ClassifyTool intentionally
// returns StageUnknown for every input. Operators must configure toolStageMap in
// policies.yaml — hardcoded names would not match every MCP server deployment.
func TestClassifyTool_AlwaysUnknown(t *testing.T) {
	for _, tool := range []string{"bash", "web_search", "read_file", "http_post", "cloud:iam", "unknown_xyz"} {
		if got := ClassifyTool(tool); got != StageUnknown {
			t.Errorf("ClassifyTool(%q) = %q, want %q (no built-in map)", tool, got, StageUnknown)
		}
	}
}

// TestClassifyToolWithMap verifies stage classification when a policy-provided map is set.
func TestClassifyToolWithMap(t *testing.T) {
	stageMap := map[string][]string{
		"recon":      {"web_search", "read_file", "list_directory"},
		"exploit":    {"bash", "execute"},
		"escalate":   {"cloud:iam"},
		"exfiltrate": {"http_post", "write_file"},
	}
	cases := []struct {
		tool  string
		stage Stage
	}{
		{"web_search", StageRecon},
		{"read_file", StageRecon},
		{"list_directory", StageRecon},
		{"bash", StageExploit},
		{"execute", StageExploit},
		{"cloud:iam", StageEscalate},
		{"http_post", StageExfiltrate},
		{"write_file", StageExfiltrate},
		{"unknown_tool_xyz", StageUnknown},
	}
	for _, c := range cases {
		if got := ClassifyToolWithMap(c.tool, stageMap); got != c.stage {
			t.Errorf("ClassifyToolWithMap(%q) = %q, want %q", c.tool, got, c.stage)
		}
	}
}

func TestDetectChain_Positive(t *testing.T) {
	b := NewBehaviorAnalyzer(20, nil)
	events := []store.ToolEvent{
		{Tool: "web_search", Stage: "recon", Timestamp: time.Now()},
		{Tool: "bash", Stage: "exploit", Timestamp: time.Now()},
		{Tool: "http_post", Stage: "exfiltrate", Timestamp: time.Now()},
	}
	if !b.DetectChain(events) {
		t.Fatal("expected attack chain to be detected")
	}
}

func TestDetectChain_Negative(t *testing.T) {
	b := NewBehaviorAnalyzer(20, nil)
	events := []store.ToolEvent{
		{Tool: "read_file", Stage: "recon", Timestamp: time.Now()},
		{Tool: "write_file", Stage: "exfiltrate", Timestamp: time.Now()},
		// No exploit stage — incomplete chain.
	}
	// recon → exfiltrate alone should not match any known 3-stage chain.
	if b.DetectChain(events) {
		t.Fatal("incomplete attack sequence should not trigger chain detection")
	}
}

func TestDetectChain_InterleavedSteps(t *testing.T) {
	b := NewBehaviorAnalyzer(20, nil)
	// Subsequence match: unknown steps in between should not break detection.
	events := []store.ToolEvent{
		{Tool: "glob", Stage: "recon", Timestamp: time.Now()},
		{Tool: "some_safe_tool", Stage: "unknown", Timestamp: time.Now()},
		{Tool: "bash", Stage: "exploit", Timestamp: time.Now()},
		{Tool: "another_safe", Stage: "unknown", Timestamp: time.Now()},
		{Tool: "http_post", Stage: "exfiltrate", Timestamp: time.Now()},
	}
	if !b.DetectChain(events) {
		t.Fatal("interleaved attack chain should still be detected via subsequence match")
	}
}
