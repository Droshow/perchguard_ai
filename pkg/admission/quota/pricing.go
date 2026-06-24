package quota

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
// on the named model. Output tokens are estimated at 3× input, reflecting the
// typical input:output ratio for agentic tool calls.
//
// Pricing as of May 2026. Update when model pricing changes.
// Unknown models fall back to a conservative $5/$15 per million tokens estimate.
func TokenCostUSD(inputTokens int, model string) float64 {
	type price struct{ inPerM, outPerM float64 }
	rates := map[string]price{
		"claude-opus-4-7":   {15.0, 75.0},
		"claude-opus-4-6":   {15.0, 75.0},
		"claude-sonnet-4-6": {3.0, 15.0},
		"claude-haiku-4-5":  {0.80, 4.0},
		"gpt-4o":            {5.0, 15.0},
		"gpt-4o-mini":       {0.15, 0.60},
		"gemini-2.5-pro":    {3.50, 10.50},
		"gemini-2.5-flash":  {0.30, 2.50},
	}
	p, ok := rates[model]
	if !ok {
		p = price{5.0, 15.0}
	}
	estimatedOutput := inputTokens * 3
	return (float64(inputTokens)*p.inPerM + float64(estimatedOutput)*p.outPerM) / 1_000_000
}
