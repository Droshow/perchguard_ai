// Package compliance maps PerchGuard's existing audit/decision artifacts onto the
// evidence shapes specific regulatory regimes expect. See
// artifacts/docs/PHASE8-AGENTIC-COMPLIANCE.md §8 for the design rationale: this package
// does not add a new enforcement path, it reads the audit store and policy profile that
// already exist and renders them in a regime's vocabulary.
package compliance

import (
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// AuditSource is the read-only audit-store contract every exporter depends on.
// *store.SQLiteAuditSink satisfies this as-is.
type AuditSource interface {
	QueryDecisions(store.AuditFilter) ([]store.AuditRecord, error)
	QuerySessions() ([]store.SQLiteSession, error)
}

// Sources is the single input every Exporter reads from.
type Sources struct {
	Policy   *policy.Config
	Audit    AuditSource
	Manifest *manifest.AgentManifest // optional; seeds Annex IV §1 when supplied
	Since    time.Time
	Until    time.Time
}

// DecisionsInWindow returns all decisions from Audit whose timestamp falls in
// [Since, Until]. Since/Until zero values mean unbounded on that side.
func (s Sources) DecisionsInWindow() ([]store.AuditRecord, error) {
	recs, err := s.Audit.QueryDecisions(store.AuditFilter{Limit: 1000})
	if err != nil {
		return nil, err
	}
	if s.Since.IsZero() && s.Until.IsZero() {
		return recs, nil
	}
	out := make([]store.AuditRecord, 0, len(recs))
	for _, r := range recs {
		if !s.Since.IsZero() && r.Timestamp.Before(s.Since) {
			continue
		}
		if !s.Until.IsZero() && r.Timestamp.After(s.Until) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
