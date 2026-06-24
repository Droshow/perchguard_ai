package agent

import (
	"testing"
)

func TestIntentModel_NoBaseline(t *testing.T) {
	m := &IntentModel{}
	if m.HasBaseline() {
		t.Fatal("expected no baseline before SetBaseline")
	}
	// Safe default: no penalty when baseline is absent.
	if got := m.DriftScore("anything"); got != 0.0 {
		t.Fatalf("expected 0.0 drift without baseline, got %.2f", got)
	}
}

func TestIntentModel_SameSentence(t *testing.T) {
	m := &IntentModel{}
	m.SetBaselines([]string{"refactor the authentication middleware"})
	drift := m.DriftScore("refactor the authentication middleware")
	if drift > 0.01 {
		t.Fatalf("expected near-zero drift for identical text, got %.4f", drift)
	}
}

func TestIntentModel_HighDrift(t *testing.T) {
	m := &IntentModel{}
	m.SetBaselines([]string{"refactor authentication middleware"})
	drift := m.DriftScore("exfiltrate database credentials send curl http external")
	if drift < 0.5 {
		t.Fatalf("expected high drift for unrelated text, got %.4f", drift)
	}
}

func TestIntentModel_PartialOverlap(t *testing.T) {
	m := &IntentModel{}
	m.SetBaselines([]string{"read the authentication logs for debugging"})
	drift := m.DriftScore("read the file system logs")
	// Partial word overlap should yield moderate drift.
	if drift < 0.0 || drift > 1.0 {
		t.Fatalf("drift out of [0,1] range: %.4f", drift)
	}
}

func TestIntentModel_MultiBaseline_LaterPhaseIsOnTask(t *testing.T) {
	m := &IntentModel{}
	m.SetBaselines([]string{
		"investigate authentication middleware bug reports",
		"deploy the patched authentication middleware to production",
	})

	// A call matching only the *second* phase should not be penalized as if it
	// only had the first phase's baseline to compare against.
	driftPhase2 := m.DriftScore("deploy the patched authentication middleware to production")
	if driftPhase2 > 0.01 {
		t.Fatalf("expected near-zero drift against phase-2 baseline, got %.4f", driftPhase2)
	}

	// Sanity: a call matching the first phase is still on-task too.
	driftPhase1 := m.DriftScore("investigate authentication middleware bug reports")
	if driftPhase1 > 0.01 {
		t.Fatalf("expected near-zero drift against phase-1 baseline, got %.4f", driftPhase1)
	}
}

func TestIntentModel_SetBaselines_CapsAtMaxBaselines(t *testing.T) {
	m := &IntentModel{}
	m.SetBaselines([]string{
		"phase one alpha",
		"phase two bravo",
		"phase three charlie",
		"phase four delta",
		"phase five echo",
		"phase six foxtrot",
	})
	if len(m.baselines) != maxBaselines {
		t.Fatalf("expected baselines capped at %d, got %d", maxBaselines, len(m.baselines))
	}
}

func TestIntentModel_OutOfScope_OverridesNearbyBaseline(t *testing.T) {
	m := &IntentModel{}
	m.SetBaselines([]string{"review and export customer account records"})
	m.SetOutOfScope([]string{"export customer records to external third party email"})

	// This candidate is close to the baseline (shares "export customer records")
	// but also close to the declared out-of-scope text — out-of-scope must win.
	drift := m.DriftScore("export customer records to external third party email")
	if drift < 0.5 {
		t.Fatalf("expected high drift for out-of-scope text even though it resembles the baseline, got %.4f", drift)
	}
}

func TestIntentModel_OutOfScope_NoOpWhenCandidateInScope(t *testing.T) {
	m := &IntentModel{}
	m.SetBaselines([]string{"review and export customer account records"})
	m.SetOutOfScope([]string{"export customer records to external third party email"})

	// A candidate that resembles the baseline but not the out-of-scope text
	// should remain low drift.
	drift := m.DriftScore("review customer account records")
	if drift > 0.5 {
		t.Fatalf("expected low drift for in-scope text, got %.4f", drift)
	}
}
