package euaiact

import (
	"strings"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/compliance"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

type fakeAuditSource struct {
	decisions []store.AuditRecord
	sessions  []store.SQLiteSession
}

func (f fakeAuditSource) QueryDecisions(store.AuditFilter) ([]store.AuditRecord, error) {
	return f.decisions, nil
}

func (f fakeAuditSource) QuerySessions() ([]store.SQLiteSession, error) {
	return f.sessions, nil
}

func TestArt26Export_ElevenRows(t *testing.T) {
	report, err := Art26Exporter{}.Export(compliance.Sources{
		Policy: &policy.Config{},
		Audit:  fakeAuditSource{},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if report.Regime != "eu-ai-act" || report.Doc != "art26-coverage" {
		t.Errorf("unexpected regime/doc: %s/%s", report.Regime, report.Doc)
	}
	if len(report.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(report.Sections))
	}
	if got := len(report.Sections[0].Rows); got != 11 {
		t.Errorf("Article 26 has 11 obligations, got %d rows", got)
	}
}

func TestRow2_DualApprovalRolesConfigured(t *testing.T) {
	p := &policy.Config{Admission: policy.AdmissionPolicy{DualApprovalRoles: []string{"compliance-officer"}}}
	row := row2(p)
	if row.Status != compliance.StatusConfigured {
		t.Errorf("expected StatusConfigured, got %s", row.Status)
	}
}

func TestRow2_HumanReviewOnlyConfigured(t *testing.T) {
	p := &policy.Config{}
	p.Policies.HumanReview.Enabled = true
	row := row2(p)
	if row.Status != compliance.StatusConfigured {
		t.Errorf("expected StatusConfigured, got %s", row.Status)
	}
}

func TestRow2_NeitherConfiguredIsPlaceholder(t *testing.T) {
	row := row2(&policy.Config{})
	if row.Status != compliance.StatusPlaceholder {
		t.Errorf("expected StatusPlaceholder when neither dualApprovalRoles nor humanReview is set, got %s", row.Status)
	}
}

func TestOverallStatus(t *testing.T) {
	withPlaceholder := []compliance.Row{
		{Status: compliance.StatusGenerated},
		{Status: compliance.StatusPlaceholder},
	}
	if got := overallStatus(withPlaceholder); got != compliance.StatusPartial {
		t.Errorf("expected StatusPartial when any row is a placeholder, got %s", got)
	}

	allGenerated := []compliance.Row{
		{Status: compliance.StatusGenerated},
		{Status: compliance.StatusConfigured},
	}
	if got := overallStatus(allGenerated); got != compliance.StatusGenerated {
		t.Errorf("expected StatusGenerated when no row is a placeholder, got %s", got)
	}
}

func TestCountByDecision(t *testing.T) {
	recs := []store.AuditRecord{
		{Decision: "ALLOW"}, {Decision: "ALLOW"}, {Decision: "MUTATE"}, {Decision: "HUMAN_REVIEW"},
	}
	counts := countByDecision(recs)
	if counts["ALLOW"] != 2 || counts["MUTATE"] != 1 || counts["HUMAN_REVIEW"] != 1 {
		t.Errorf("unexpected counts: %+v", counts)
	}
}

func TestArt26Export_DecisionCountsFeedRows(t *testing.T) {
	report, err := Art26Exporter{}.Export(compliance.Sources{
		Policy: &policy.Config{},
		Audit: fakeAuditSource{decisions: []store.AuditRecord{
			{Decision: "MUTATE"}, {Decision: "HUMAN_REVIEW"}, {Decision: "HUMAN_REVIEW"}, {Decision: "TERMINATE"},
		}},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	rows := report.Sections[0].Rows
	if !strings.Contains(rows[2].Evidence, "1 MUTATE") { // row3
		t.Errorf("row3 evidence missing mutate count: %q", rows[2].Evidence)
	}
	if !strings.Contains(rows[3].Evidence, "2 HUMAN_REVIEW") || !strings.Contains(rows[3].Evidence, "1 TERMINATE") { // row4
		t.Errorf("row4 evidence missing review/terminate counts: %q", rows[3].Evidence)
	}
}
