package agent

import (
	"math"
	"strings"
	"unicode"
)

// IntentModel tracks the agent's declared intent and scores semantic drift.
// Implemented as word-frequency cosine similarity — no external ML dependencies.
// Drift 0.0 = on-task, 1.0 = completely unrelated.
type IntentModel struct {
	// baselines holds one word-frequency vector per declared phase (fused
	// Summary+Scope+PRD text plus up to 4 manifest-declared phases — see
	// AgentManifest.Mission.Phases, capped at 5 total by manifest validation).
	// A call is "on task" if it is close to ANY of these.
	baselines []map[string]float64

	// outOfScope is the word-frequency vector of the manifest's declared
	// out-of-scope vocabulary (Mission.OutOfScope). Nil when none declared.
	outOfScope map[string]float64
}

// maxBaselines bounds the number of drift baselines: the fused Summary+Scope+PRD
// text plus up to 4 manifest-declared phases (see pkg/manifest.Mission.Phases,
// capped at 4 by manifest validation). Enforced here too as defense-in-depth —
// SetBaselines must not silently lose this bound if a future caller seeds
// baselines without going through manifest validation.
const maxBaselines = 5

// SetBaselines replaces the baseline set with one word-frequency vector per
// non-empty text, up to maxBaselines. Empty strings are skipped.
//
// Multiple baselines exist so a long-running agent doesn't false-positive
// against a single registration-time snapshot as it legitimately progresses
// through later phases of its own declared task (see
// EUAIACT-PERCHGUARD-SYNERGY.md §3). Because DriftScore takes the *minimum*
// distance across baselines, every additional baseline can only ever widen
// what counts as "on task" — a manifest author could otherwise dial drift
// toward zero for free by declaring more plausible-sounding phases.
// SetOutOfScope provides the counterweight: see DriftScore.
func (m *IntentModel) SetBaselines(texts []string) {
	m.baselines = nil
	for _, t := range texts {
		if t == "" {
			continue
		}
		if len(m.baselines) >= maxBaselines {
			break
		}
		m.baselines = append(m.baselines, tokenizeWords(t))
	}
}

// SetOutOfScope captures the manifest's declared out-of-scope vocabulary.
// A no-op (clears any prior value) when texts is empty.
func (m *IntentModel) SetOutOfScope(texts []string) {
	if len(texts) == 0 {
		m.outOfScope = nil
		return
	}
	m.outOfScope = tokenizeWords(strings.Join(texts, " "))
}

// HasBaseline reports whether at least one baseline has been established.
func (m *IntentModel) HasBaseline() bool {
	return len(m.baselines) > 0
}

// DriftScore returns how far the candidate text has drifted from the declared
// intent. Returns 0.0 when no baseline is set (safe default — do not penalise).
//
// minDist is the minimum distance to any declared-phase baseline — a call close
// to ANY phase counts as on-task. If the candidate resembles the declared
// out-of-scope vocabulary more than it resembles its nearest baseline
// (cosineSimilarity(outOfScope, vec) > minDist), that resemblance is returned as
// the drift score instead. This bounds how far declaring extra phases can shrink
// the drift signal: Phases can only widen the in-policy region, OutOfScope can
// only narrow it back.
//
// The override value (oosSim) is used as-is, with no separate threshold or
// scaling — it is whatever cosineSimilarity(outOfScope, vec) returns. The
// direction is correct (out-of-scope resemblance can only push drift up, never
// down), but the magnitude is not independently calibrated against
// DriftThreshold; treat it as a relative signal, not a tuned absolute score.
func (m *IntentModel) DriftScore(candidate string) float64 {
	if len(m.baselines) == 0 {
		return 0.0
	}
	vec := tokenizeWords(candidate)

	minDist := 1.0
	for _, baseline := range m.baselines {
		if dist := 1.0 - cosineSimilarity(baseline, vec); dist < minDist {
			minDist = dist
		}
	}

	if len(m.outOfScope) > 0 {
		if oosSim := cosineSimilarity(m.outOfScope, vec); oosSim > minDist {
			return oosSim
		}
	}

	return minDist
}

// tokenizeWords converts text into a normalized word-frequency map.
// Words shorter than 3 characters are dropped to filter stop-word noise.
func tokenizeWords(text string) map[string]float64 {
	freq := make(map[string]float64)
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		if len(w) > 2 {
			freq[w]++
		}
	}
	return freq
}

// cosineSimilarity computes the cosine similarity between two word-frequency vectors.
// Returns 0.0 when either vector is empty.
func cosineSimilarity(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	var dot, normA, normB float64
	for word, wa := range a {
		normA += wa * wa
		if wb, ok := b[word]; ok {
			dot += wa * wb
		}
	}
	for _, wb := range b {
		normB += wb * wb
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
