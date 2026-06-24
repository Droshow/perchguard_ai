package compliance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleReport() Report {
	return Report{
		Regime:      "eu-ai-act",
		Doc:         "art26-coverage",
		GeneratedAt: time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC),
		Sections: []Section{
			{
				Title:  "Article 26 — Deployer Obligations Coverage",
				Status: StatusGenerated,
				Body:   "Eleven obligations.",
				Rows: []Row{
					{Obligation: "(1) Use per instructions", Status: StatusConfigured, Evidence: "policy profile", Source: "configs/profiles/<vertical>.yaml"},
				},
			},
		},
	}
}

func TestRenderMarkdown(t *testing.T) {
	out := RenderMarkdown(sampleReport())

	for _, want := range []string{
		"# EU-AI-ACT — art26-coverage",
		"Article 26 — Deployer Obligations Coverage",
		"`generated`",
		"| Obligation | Status | Evidence | Source |",
		"(1) Use per instructions",
		"`configured`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderMarkdown output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRenderMarkdownOmitsEmptyRowsTable(t *testing.T) {
	r := sampleReport()
	r.Sections[0].Rows = nil
	out := RenderMarkdown(r)
	if strings.Contains(out, "| Obligation |") {
		t.Error("RenderMarkdown should not emit a rows table when Rows is empty")
	}
}

func TestRenderJSONRoundTrip(t *testing.T) {
	want := sampleReport()
	data, err := RenderJSON(want)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Regime != want.Regime || got.Doc != want.Doc {
		t.Errorf("round trip mismatch: got %+v, want %+v", got, want)
	}
	if len(got.Sections) != 1 || len(got.Sections[0].Rows) != 1 {
		t.Fatalf("round trip lost sections/rows: %+v", got)
	}
	if got.Sections[0].Rows[0].Obligation != want.Sections[0].Rows[0].Obligation {
		t.Errorf("row mismatch: got %q, want %q", got.Sections[0].Rows[0].Obligation, want.Sections[0].Rows[0].Obligation)
	}
}
