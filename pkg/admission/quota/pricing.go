package quota

import "regexp"

type modelPrice struct{ inPerM, outPerM float64 }

// Pricing as of May 2026. Update when model pricing changes.
var modelRates = map[string]modelPrice{
	"claude-opus-4-7":   {15.0, 75.0},
	"claude-opus-4-6":   {15.0, 75.0},
	"claude-sonnet-4-6": {3.0, 15.0},
	"claude-haiku-4-5":  {0.80, 4.0},
	"gpt-4o":            {5.0, 15.0},
	"gpt-4o-mini":       {0.15, 0.60},
	"gemini-2.5-pro":    {3.50, 10.50},
	"gemini-2.5-flash":  {0.30, 2.50},
}

var dateSnapshotSuffix = regexp.MustCompile(`-\d{8}$`)

// modelRate resolves per-million-token pricing for model, falling back to a
// conservative $5/$15 estimate for unknown models. Anthropic model IDs are often
// pinned to a dated snapshot (e.g. "claude-haiku-4-5-20251001") that won't match
// the table verbatim — strip that suffix before giving up.
func modelRate(model string) modelPrice {
	if p, ok := modelRates[model]; ok {
		return p
	}
	if stripped := dateSnapshotSuffix.ReplaceAllString(model, ""); stripped != model {
		if p, ok := modelRates[stripped]; ok {
			return p
		}
	}
	return modelPrice{5.0, 15.0}
}

// EstimateTokens returns an estimated input token count from a raw JSON payload.
// Uses byte-length / 4 — the standard GPT tokenization approximation.
// Minimum 1 so the quota accumulator always advances on every call.
func EstimateTokens(payload []byte) int {
	n := len(payload) / 4
	if n < 1 {
		return 1
	}
	return n
}

// TokenCostUSD returns the estimated USD cost for the given number of input tokens
// on the named model, for callers with only a request-size estimate (no real usage
// data from the provider — e.g. the /mcp transparent-proxy path). Output tokens are
// estimated at 3x input, reflecting the typical input:output ratio for agentic tool
// calls. Prefer TokenCostUSDExact when real usage.input_tokens/output_tokens are
// available.
func TokenCostUSD(inputTokens int, model string) float64 {
	p := modelRate(model)
	estimatedOutput := inputTokens * 3
	return (float64(inputTokens)*p.inPerM + float64(estimatedOutput)*p.outPerM) / 1_000_000
}

// TokenCostUSDExact returns the exact USD cost for real input/output token counts
// reported by the LLM provider's own response (e.g. Anthropic's response.usage) —
// no estimation involved.
func TokenCostUSDExact(inputTokens, outputTokens int, model string) float64 {
	p := modelRate(model)
	return (float64(inputTokens)*p.inPerM + float64(outputTokens)*p.outPerM) / 1_000_000
}
