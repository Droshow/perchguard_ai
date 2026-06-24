package euaiact

import (
	"strings"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/compliance"
	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

func TestAnnexIVExport_NineSections(t *testing.T) {
	report, err := AnnexIVExporter{}.Export(compliance.Sources{
		Policy: &policy.Config{},
		Audit:  fakeAuditSource{},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if report.Regime != "eu-ai-act" || report.Doc != "annex-iv" {
		t.Errorf("unexpected regime/doc: %s/%s", report.Regime, report.Doc)
	}
	// 9 Annex IV points, but §2 is split into three sub-sections (2(a-d,f,h), 2e, 2g),
	// so the rendered section list has 11 entries.
	if got := len(report.Sections); got != 11 {
		t.Errorf("expected 11 rendered sections (9 points, §2 split into 3), got %d", got)
	}
}

func TestSection1_NoManifest(t *testing.T) {
	sec := section1(compliance.Sources{})
	if sec.Status != compliance.StatusPlaceholder {
		t.Errorf("expected placeholder without a manifest, got %s", sec.Status)
	}
	if !strings.Contains(sec.Body, "No AgentManifest supplied") {
		t.Errorf("expected body to note the missing manifest, got %q", sec.Body)
	}
}

func TestSection1_SeededFromManifest(t *testing.T) {
	m := &manifest.AgentManifest{Mission: manifest.Mission{Summary: "Reconcile ledger entries nightly"}}
	sec := section1(compliance.Sources{Manifest: m})
	if !strings.Contains(sec.Body, "Reconcile ledger entries nightly") {
		t.Errorf("expected body to include the manifest's mission summary, got %q", sec.Body)
	}
}

func TestSection9_DecisionAndSessionCounts(t *testing.T) {
	sec := section9(
		compliance.Sources{},
		map[string]int{"ALLOW": 3, "DENY": 1, "MUTATE": 2, "HUMAN_REVIEW": 1, "TERMINATE": 0},
		[]store.SQLiteSession{{ID: "s1"}, {ID: "s2"}},
	)
	for _, want := range []string{"ALLOW=3", "DENY=1", "MUTATE=2", "HUMAN_REVIEW=1", "TERMINATE=0", "2 tracked sessions"} {
		if !strings.Contains(sec.Body, want) {
			t.Errorf("section9 body missing %q: %q", want, sec.Body)
		}
	}
}

func TestSection2e_HumanReviewSettings(t *testing.T) {
	p := &policy.Config{}
	p.Policies.HumanReview.Enabled = true
	p.Policies.HumanReview.TimeoutSeconds = 120
	sec := section2e(compliance.Sources{Policy: p}, map[string]int{"HUMAN_REVIEW": 4})
	if sec.Status != compliance.StatusGenerated {
		t.Errorf("expected StatusGenerated, got %s", sec.Status)
	}
	for _, want := range []string{"enabled=true", "timeoutSeconds=120", "4 HUMAN_REVIEW"} {
		if !strings.Contains(sec.Body, want) {
			t.Errorf("section2e body missing %q: %q", want, sec.Body)
		}
	}
}
