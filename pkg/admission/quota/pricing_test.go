package quota

import (
	"math"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantMin int
	}{
		{"empty payload returns minimum 1", []byte{}, 1},
		{"nil payload returns minimum 1", nil, 1},
		{"small payload rounds up to 1", []byte("hi"), 1},
		{"100-byte payload gives ~25", []byte(`{"path":"/workspace/main.go","action":"read"}`), 11},
		{"1000-byte payload gives ~250", make([]byte, 1000), 250},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateTokens(tc.payload)
			if got < 1 {
				t.Errorf("EstimateTokens returned %d, want >= 1", got)
			}
			if tc.wantMin > 1 && got < tc.wantMin {
				t.Errorf("got %d, want >= %d", got, tc.wantMin)
			}
		})
	}
}

func TestTokenCostUSD_KnownModel(t *testing.T) {
	// claude-sonnet-4-6: $3/M input, $15/M output; output estimated at 3× input
	// 1000 input tokens → $0.003 input + $0.045 output = $0.048
	cost := TokenCostUSD(1000, "claude-sonnet-4-6")
	want := 0.048
	if math.Abs(cost-want) > 0.0001 {
		t.Errorf("claude-sonnet-4-6 1000 tokens: got $%.6f, want $%.6f", cost, want)
	}
}

func TestTokenCostUSD_UnknownModelFallback(t *testing.T) {
	// Unknown model uses conservative $5/M input, $15/M output
	// 1000 input → $0.005 + $0.045 = $0.05
	cost := TokenCostUSD(1000, "unknown-model-xyz")
	want := 0.05
	if math.Abs(cost-want) > 0.0001 {
		t.Errorf("unknown model 1000 tokens: got $%.6f, want $%.6f", cost, want)
	}
}

func TestTokenCostUSD_ZeroTokens(t *testing.T) {
	if cost := TokenCostUSD(0, "claude-sonnet-4-6"); cost != 0 {
		t.Errorf("zero tokens should cost $0, got $%f", cost)
	}
}

func TestTokenCostUSD_CheaperThanExpensive(t *testing.T) {
	haiku := TokenCostUSD(10000, "claude-haiku-4-5")
	opus := TokenCostUSD(10000, "claude-opus-4-7")
	if haiku >= opus {
		t.Errorf("haiku ($%.4f) should cost less than opus ($%.4f)", haiku, opus)
	}
}
