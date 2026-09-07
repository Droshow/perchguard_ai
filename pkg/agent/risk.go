package agent

import "sync"

// Risk thresholds that map cumulative score to pipeline decisions.
// HUMAN_REVIEW fires at 0.7; TERMINATE fires at 0.9.
const (
	RiskThresholdHumanReview = 0.7
	RiskThresholdTerminate   = 0.9
)

// RiskSignal is a single risk contribution with an explanation.
type RiskSignal struct {
	Contribution float64
	Reason       string
}

// RiskAccumulator maintains a cumulative 0.0–1.0 session risk score.
// Risk only ever increases within a session — partial recovery is intentional by design
// (a session that drifted once remains suspect even if it temporarily behaves).
type RiskAccumulator struct {
	mu    sync.Mutex
	score float64
}

// Add incorporates a risk signal, clamping the total to 1.0.
func (r *RiskAccumulator) Add(signal RiskSignal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.score += signal.Contribution
	if r.score > 1.0 {
		r.score = 1.0
	}
}

// Score returns the current cumulative risk score.
func (r *RiskAccumulator) Score() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.score
}

// Severity returns the OTEL-compatible severity label for the current score.
func (r *RiskAccumulator) Severity() string {
	s := r.Score()
	switch {
	case s >= RiskThresholdTerminate:
		return "critical"
	case s >= RiskThresholdHumanReview:
		return "high"
	case s >= 0.4:
		return "medium"
	default:
		return "low"
	}
}
