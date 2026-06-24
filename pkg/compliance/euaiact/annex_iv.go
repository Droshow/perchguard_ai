package euaiact

import (
	"fmt"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/agent"
	"github.com/Droshow/PerchGuard/perchguard/pkg/compliance"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

func init() {
	compliance.Register("eu-ai-act", "annex-iv", AnnexIVExporter{})
}

// AnnexIVExporter produces the Annex IV technical documentation export — 9 sections
// per Article 11(1). Three sections (§5, §9, the Art. 14 portion of §2(e)/§3) are
// generated from the audit store and policy profile; the rest ship as labeled
// placeholders with an upstream-source pointer.
type AnnexIVExporter struct{}

func (AnnexIVExporter) Export(s compliance.Sources) (compliance.Report, error) {
	decisions, err := s.DecisionsInWindow()
	if err != nil {
		return compliance.Report{}, err
	}
	sessions, err := s.Audit.QuerySessions()
	if err != nil {
		return compliance.Report{}, err
	}
	counts := countByDecision(decisions)

	sections := []compliance.Section{
		section1(s),
		placeholderSection("§2(a)-(d),(f),(h) Development process", "Customer-authored — methods/steps, third-party pre-trained components, design specs, architecture, training-data datasheets, planned changes, cybersecurity measures. §2(a)'s third-party-model checklist feeds from the Title V/Annex XII vendor-disclosure request."),
		section2e(s, counts),
		section2g(),
		section3(s, counts),
		placeholderSection("§4 Appropriateness of performance metrics", "Model-quality territory, outside PerchGuard's runtime scope."),
		section5(s),
		section6(),
		placeholderSection("§7 Harmonised standards applied / alternatives", "Standards-conformity territory (Art. 40-46), outside PerchGuard's scope."),
		placeholderSection("§8 Copy of EU declaration of conformity (Art. 47)", "Customer-authored; this export package has a slot for it."),
		section9(s, counts, sessions),
	}

	return compliance.Report{
		Regime:      "eu-ai-act",
		Doc:         "annex-iv",
		GeneratedAt: time.Now(),
		Sections:    sections,
	}, nil
}

func section1(s compliance.Sources) compliance.Section {
	if s.Manifest != nil {
		return compliance.Section{
			Title:  "§1 General description",
			Status: compliance.StatusPlaceholder,
			Body: fmt.Sprintf(
				"Seeded from AgentManifest: intended purpose = %q. Hardware, deployment form, and UI description remain customer-authored — outside PerchGuard's runtime scope.",
				s.Manifest.Mission.Summary,
			),
		}
	}
	return placeholderSection("§1 General description",
		"Intended purpose, provider/version, system interactions, hardware, deployment form, UI, instructions for use. No AgentManifest supplied — pass --manifest to seed intended purpose from Mission.Summary.")
}

func section2e(s compliance.Sources, counts map[string]int) compliance.Section {
	enabled := s.Policy != nil && s.Policy.Policies.HumanReview.Enabled
	timeout := 0
	if s.Policy != nil {
		timeout = s.Policy.Policies.HumanReview.TimeoutSeconds
	}
	return compliance.Section{
		Title:  "§2(e) Assessment of Art. 14 human-oversight measures",
		Status: compliance.StatusGenerated,
		Body: fmt.Sprintf(
			"humanReview.enabled=%v, timeoutSeconds=%d. %d HUMAN_REVIEW decisions in window. Each decision's Reason/PolicyHit fields are the technical measures facilitating deployer interpretation of outputs.",
			enabled, timeout, counts["HUMAN_REVIEW"],
		),
	}
}

func section2g() compliance.Section {
	return compliance.Section{
		Title:  "§2(g) Validation/testing procedures, test logs and reports",
		Status: compliance.StatusPartial,
		Body:   "Phase 5 adversarial-validation findings (artifacts/docs/PHASE5-ADVERSARIAL-VALIDATION.md, 15/18 pass) and cmd/redteam-agent/ scenario runs are test logs and reports for the governance-control dimension. They do not substitute for model-accuracy/discrimination metrics, which remain customer/vendor-authored.",
	}
}

func section3(s compliance.Sources, counts map[string]int) compliance.Section {
	return compliance.Section{
		Title:  "§3 Monitoring/control: capabilities, limitations, oversight, input-data specs",
		Status: compliance.StatusPartial,
		Body: fmt.Sprintf(
			"Art. 14 oversight portion overlaps §2(e) above (%d HUMAN_REVIEW decisions in window). Foreseeable unintended outcomes can draw on Phase 5 findings (e.g. F1: semantic-firewall gap on SSN exfiltration) as a documented, evidenced limitation. Accuracy-by-population and full input-data specs remain customer-authored.",
			counts["HUMAN_REVIEW"],
		),
	}
}

func section5(s compliance.Sources) compliance.Section {
	patterns := 0
	driftThreshold := 0.0
	if s.Policy != nil {
		patterns = len(s.Policy.Policies.AgentFleet.BehaviorPatterns)
		driftThreshold = s.Policy.Policies.AgentFleet.DriftThreshold
	}
	return compliance.Section{
		Title:  "§5 Risk management system (Art. 9)",
		Status: compliance.StatusGenerated,
		Body: fmt.Sprintf(
			"policies.admission/humanReview/audit define the risk-management structure. %d named behavior patterns configured (driftThreshold=%.2f). Risk thresholds: HUMAN_REVIEW fires at %.2f, TERMINATE fires at %.2f (pkg/agent/risk.go).",
			patterns, driftThreshold, agent.RiskThresholdHumanReview, agent.RiskThresholdTerminate,
		),
	}
}

func section6() compliance.Section {
	return compliance.Section{
		Title:  "§6 Description of relevant lifecycle changes",
		Status: compliance.StatusPartial,
		Body:   "perchguard_policy_reload_total and the policy-file version history (policy_version on each audit record) document governance-layer changes. Model/system changes remain customer-authored.",
	}
}

func section9(s compliance.Sources, counts map[string]int, sessions []store.SQLiteSession) compliance.Section {
	return compliance.Section{
		Title:  "§9 Post-market monitoring system (Art. 72)",
		Status: compliance.StatusGenerated,
		Body: fmt.Sprintf(
			"Decision counts in window: ALLOW=%d DENY=%d MUTATE=%d HUMAN_REVIEW=%d TERMINATE=%d, across %d tracked sessions. The audit store's query surface (this export, /api/audit) constitutes the monitoring system; the recurring export cadence constitutes the monitoring plan.",
			counts["ALLOW"], counts["DENY"], counts["MUTATE"], counts["HUMAN_REVIEW"], counts["TERMINATE"], len(sessions),
		),
	}
}

func placeholderSection(title, body string) compliance.Section {
	return compliance.Section{Title: title, Status: compliance.StatusPlaceholder, Body: body}
}
