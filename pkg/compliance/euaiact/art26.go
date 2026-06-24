// Package euaiact implements EU AI Act exports. The row/section content here is a
// direct transcription of the coverage tables already authored in
// artifacts/docs/EUAIACT-COMPLIANCE-ARTICLE-MAP.md (Art. 26: lines 618-637; Annex IV:
// lines 1414-1433) — this file does not re-derive the legal mapping, it codifies it.
package euaiact

import (
	"fmt"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/compliance"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

func init() {
	compliance.Register("eu-ai-act", "art26-coverage", Art26Exporter{})
}

// Art26Exporter produces the Article 26 deployer-obligations coverage report —
// 11 rows, one per Art. 26(1)-(11).
type Art26Exporter struct{}

func (Art26Exporter) Export(s compliance.Sources) (compliance.Report, error) {
	decisions, err := s.DecisionsInWindow()
	if err != nil {
		return compliance.Report{}, err
	}
	counts := countByDecision(decisions)
	mutateCount := counts["MUTATE"]
	reviewCount := counts["HUMAN_REVIEW"]
	terminateCount := counts["TERMINATE"]

	var policy = s.Policy

	rows := []compliance.Row{
		row1(policy),
		row2(policy),
		row3(policy, mutateCount),
		row4(reviewCount, terminateCount),
		row5(policy),
		{
			Obligation: "(6) Inform workers' representatives and affected workers before workplace deployment",
			Status:     compliance.StatusOutsideLayer,
			Evidence:   "HR/communications process, not a PerchGuard artifact. Policy profile agentRoles are a useful input to disclosure.",
			Source:     "configs/profiles/<vertical>.yaml policies.toolAuthorization.agentRoles",
		},
		{
			Obligation: "(7) Register in EU database (Annex VIII) — Annex III 5(b)/(c) only",
			Status:     compliance.StatusNotApplicable,
			Evidence:   "Applies only if this deployment is an Annex III 5(b)/(c) system (creditworthiness, life/health insurance risk). No Annex III risk-tagging is wired up yet, so this is a static placeholder pending manual classification.",
			Source:     "POST /agents/register session data overlaps Annex VIII §C fields",
		},
		{
			Obligation: "(8) Feed Art. 13 info into GDPR Art. 35 DPIA, where applicable",
			Status:     compliance.StatusOutsideLayer,
			Evidence:   "DPIA is a separate legal artifact; PerchGuard's data-flow/lineage data is a useful input to it, not a substitute.",
			Source:     "Art. 10 data-and-data-governance mapping",
		},
		{
			Obligation: "(9) Real-time biometric ID by law enforcement — authorisation/reporting",
			Status:     compliance.StatusNotApplicable,
			Evidence:   "Out of scope for nearly all deployments — applies only if Annex III point 1(a) applies.",
			Source:     "n/a",
		},
		{
			Obligation: "(10) Inform natural persons subject to a high-risk AI decision",
			Status:     compliance.StatusOutsideLayer,
			Evidence:   "Maps to the Art. 50 transparency-disclosure obligation — a separate export, not part of this report.",
			Source:     "Art. 50 transparency obligations",
		},
		row11(),
	}

	return compliance.Report{
		Regime:      "eu-ai-act",
		Doc:         "art26-coverage",
		GeneratedAt: time.Now(),
		Sections: []compliance.Section{
			{
				Title:  "Article 26 — Deployer Obligations Coverage",
				Status: overallStatus(rows),
				Body:   "Eleven obligations under Art. 26(1)-(11), each with a status, evidence pointer, and the PerchGuard surface that backs it. Generated from the policy profile and audit store currently configured — not a legal certification.",
				Rows:   rows,
			},
		},
	}, nil
}

func row1(p *policy.Config) compliance.Row {
	return compliance.Row{
		Obligation: "(1) Use the system per the provider's instructions, via appropriate technical/organisational measures",
		Status:     compliance.StatusConfigured,
		Evidence:   "The policy profile is the codified set of technical/organisational measures.",
		Source:     "configs/profiles/<vertical>.yaml",
	}
}

func row2(p *policy.Config) compliance.Row {
	if p != nil && len(p.Admission.DualApprovalRoles) > 0 {
		return compliance.Row{
			Obligation: "(2) Assign human oversight to competent, trained, authorised staff",
			Status:     compliance.StatusConfigured,
			Evidence:   fmt.Sprintf("dualApprovalRoles configured: %v", p.Admission.DualApprovalRoles),
			Source:     "policies.admission.dualApprovalRoles",
		}
	}
	if p != nil && p.Policies.HumanReview.Enabled {
		return compliance.Row{
			Obligation: "(2) Assign human oversight to competent, trained, authorised staff",
			Status:     compliance.StatusConfigured,
			Evidence:   "humanReview enabled (single-approver REVIEW path); no dualApprovalRoles configured.",
			Source:     "policies.humanReview",
		}
	}
	return compliance.Row{
		Obligation: "(2) Assign human oversight to competent, trained, authorised staff",
		Status:     compliance.StatusPlaceholder,
		Evidence:   "No humanReview or dualApprovalRoles configured in the loaded policy.",
		Source:     "policies.admission.dualApprovalRoles / policies.humanReview",
	}
}

func row3(p *policy.Config, mutateCount int) compliance.Row {
	enabled := p != nil && p.Policies.ParameterSanitization.Enabled
	return compliance.Row{
		Obligation: "(3) Ensure input data is relevant/representative, to the extent the deployer controls it",
		Status:     compliance.StatusEvidenced,
		Evidence:   fmt.Sprintf("ParameterSanitization enabled=%v; %d MUTATE decisions in window (sanitizer activity).", enabled, mutateCount),
		Source:     "pkg/admission/mutator/parameter_sanitizer.go + audit store",
	}
}

func row4(reviewCount, terminateCount int) compliance.Row {
	return compliance.Row{
		Obligation: "(4) Monitor operation; suspend use & notify on risk (Art. 79(1))",
		Status:     compliance.StatusEvidenced,
		Evidence:   fmt.Sprintf("%d HUMAN_REVIEW decisions, %d TERMINATE decisions in window.", reviewCount, terminateCount),
		Source:     "audit store decisions table",
	}
}

func row5(p *policy.Config) compliance.Row {
	enabled := p != nil && p.Policies.Audit.Enabled
	return compliance.Row{
		Obligation: "(5) Retain Art. 19 logs ≥ 6 months, to the extent under deployer control",
		Status:     compliance.StatusEvidenced,
		Evidence:   fmt.Sprintf("audit.enabled=%v; SQLite audit store is the durable retention mechanism.", enabled),
		Source:     "pkg/store/sqlite.go",
	}
}

func row11() compliance.Row {
	return compliance.Row{
		Obligation: "(11) Cooperate with competent authorities",
		Status:     compliance.StatusEvidenced,
		Evidence:   "Audit store is queryable on demand via this export and /api/audit.",
		Source:     "/api/audit, perchguard --mode=compliance",
	}
}

func overallStatus(rows []compliance.Row) compliance.Status {
	for _, r := range rows {
		if r.Status == compliance.StatusPlaceholder {
			return compliance.StatusPartial
		}
	}
	return compliance.StatusGenerated
}

func countByDecision(recs []store.AuditRecord) map[string]int {
	counts := map[string]int{}
	for _, r := range recs {
		counts[r.Decision]++
	}
	return counts
}
