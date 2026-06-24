package compliance

import "time"

// Status mirrors the vocabulary already used in
// artifacts/docs/EUAIACT-COMPLIANCE-ARTICLE-MAP.md's coverage tables.
type Status string

const (
	StatusGenerated     Status = "generated"     // fully populated from audit/policy data
	StatusPartial       Status = "partial"       // populated, but only covers part of the requirement
	StatusConfigured    Status = "configured"    // a config surface exists and is set
	StatusEvidenced     Status = "evidenced"     // backed by audit-store data, not just config
	StatusPlaceholder   Status = "placeholder"   // customer/vendor-authored; PerchGuard has no data
	StatusOutsideLayer  Status = "outside_layer" // workflow/process obligation, not a PerchGuard artifact
	StatusNotApplicable Status = "not_applicable"
)

// Row is one line item in an obligations-coverage table (e.g. Art. 26's 11 rows).
type Row struct {
	Obligation string `json:"obligation"`
	Status     Status `json:"status"`
	Evidence   string `json:"evidence"`
	Source     string `json:"source"`
}

// Section is one block of a structured document export (e.g. one of Annex IV's 9 points).
type Section struct {
	Title  string `json:"title"`
	Status Status `json:"status"`
	Body   string `json:"body"`
	Rows   []Row  `json:"rows,omitempty"`
}

// Report is the regime-agnostic output shape every Exporter produces.
type Report struct {
	Regime      string    `json:"regime"`
	Doc         string    `json:"doc"`
	GeneratedAt time.Time `json:"generated_at"`
	Sections    []Section `json:"sections"`
}
