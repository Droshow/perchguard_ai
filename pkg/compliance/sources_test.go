package compliance

import (
	"testing"
	"time"

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

func recordAt(ts time.Time) store.AuditRecord {
	return store.AuditRecord{RequestUID: ts.String(), Timestamp: ts}
}

func TestDecisionsInWindow_Unbounded(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := Sources{Audit: fakeAuditSource{decisions: []store.AuditRecord{
		recordAt(base), recordAt(base.Add(24 * time.Hour)),
	}}}

	got, err := src.DecisionsInWindow()
	if err != nil {
		t.Fatalf("DecisionsInWindow: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 records with no bounds set, got %d", len(got))
	}
}

func TestDecisionsInWindow_FiltersOutsideBounds(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := Sources{
		Audit: fakeAuditSource{decisions: []store.AuditRecord{
			recordAt(base.Add(-1 * time.Hour)), // before Since
			recordAt(base),                     // in window
			recordAt(base.Add(1 * time.Hour)),  // in window
			recordAt(base.Add(3 * time.Hour)),  // after Until
		}},
		Since: base,
		Until: base.Add(2 * time.Hour),
	}

	got, err := src.DecisionsInWindow()
	if err != nil {
		t.Fatalf("DecisionsInWindow: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 records inside [Since, Until], got %d", len(got))
	}
	for _, r := range got {
		if r.Timestamp.Before(src.Since) || r.Timestamp.After(src.Until) {
			t.Errorf("record %v fell outside the requested window", r.Timestamp)
		}
	}
}
