package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type meterConfig struct {
	addr     string
	apiKey   string
	session  string
	interval time.Duration
	once     bool
}

// ── API response shapes (local copies — meter is self-contained, no pkg/api import) ──

type meterSessionList []struct {
	SessionID string `json:"session_id"`
}

type meterBudget struct {
	SessionID      string  `json:"session_id"`
	ElapsedMinutes float64 `json:"elapsed_minutes"`
	EstTokens      int     `json:"estimated_tokens"`
	EstCostUSD     float64 `json:"estimated_cost_usd"`
	ToolCalls      int     `json:"tool_calls"`
	CallsLastMin   int     `json:"calls_last_minute"`
	Limits         struct {
		MaxTokens int     `json:"maxTokensPerSession"`
		MaxCost   float64 `json:"maxCostPerSessionUSD"`
		MaxCalls  int     `json:"maxToolCallsPerSession"`
		MaxPerMin int     `json:"maxToolCallsPerMinute"`
	} `json:"limits"`
}

// ── runner ────────────────────────────────────────────────────────────────────

func runMeter(cfg meterConfig) {
	draw := func() {
		if !cfg.once {
			fmt.Print("\033[H\033[2J") // clear screen, cursor home
		}
		renderMeter(cfg)
	}

	draw()
	if cfg.once {
		return
	}

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()
	for range ticker.C {
		draw()
	}
}

func renderMeter(cfg meterConfig) {
	now := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("PerchGuard Budget Meter  %s\n", now)

	// Collect session IDs to render.
	var ids []string
	if cfg.session != "" {
		ids = []string{cfg.session}
	} else {
		var list meterSessionList
		if err := apiGet(http.DefaultClient, watchConfig{addr: cfg.addr, apiKey: cfg.apiKey}, "/api/sessions", &list); err != nil {
			fmt.Printf("\n  ✗ cannot reach %s: %v\n", cfg.addr, err)
			fmt.Println("  Is PerchGuard running? Is PERCHGUARD_API_KEY set?")
			if !cfg.once {
				fmt.Printf("\n  Retrying in %s — Ctrl-C to exit\n", cfg.interval)
			}
			return
		}
		for _, s := range list {
			ids = append(ids, s.SessionID)
		}
	}

	if len(ids) == 0 {
		fmt.Println(strings.Repeat("─", 72))
		fmt.Println("  No active sessions.")
		if !cfg.once {
			fmt.Printf("\n  Waiting for sessions — Ctrl-C to exit\n")
		}
		return
	}

	for _, id := range ids {
		var b meterBudget
		path := "/api/sessions/" + id + "/budget"
		if err := apiGet(http.DefaultClient, watchConfig{addr: cfg.addr, apiKey: cfg.apiKey}, path, &b); err != nil {
			fmt.Println(strings.Repeat("─", 72))
			fmt.Printf("  %s  [no budget data yet]\n", truncate(id, 24))
			continue
		}
		renderBudgetSession(b, cfg.once)
	}

	if !cfg.once {
		fmt.Printf("\nRefreshing every %s — Ctrl-C to exit\n", cfg.interval)
	}
}

func renderBudgetSession(b meterBudget, compact bool) {
	const barWidth = 32

	pctTokens := meterPct(float64(b.EstTokens), float64(b.Limits.MaxTokens))
	pctCost := meterPct(b.EstCostUSD, b.Limits.MaxCost)
	pctCalls := meterPct(float64(b.ToolCalls), float64(b.Limits.MaxCalls))
	pctRate := meterPct(float64(b.CallsLastMin), float64(b.Limits.MaxPerMin))

	// Compact single-line mode for tmux status bars / shell prompts.
	if compact {
		fmt.Printf("%s  tok %s  $%.2f  calls %s\n",
			truncate(b.SessionID, 20),
			meterFmtPct(pctTokens),
			b.EstCostUSD,
			meterFmtPct(pctCalls),
		)
		return
	}

	fmt.Println(strings.Repeat("─", 72))
	fmt.Printf("  Session: %-24s  Elapsed: %.1f min\n\n", b.SessionID, b.ElapsedMinutes)

	fmt.Printf("  Tokens  %s%s\033[0m  %s / %s  (%s)\n",
		meterColor(pctTokens), meterBar(pctTokens, barWidth),
		meterComma(b.EstTokens), meterComma(b.Limits.MaxTokens),
		meterFmtPct(pctTokens),
	)
	fmt.Printf("  Cost    %s%s\033[0m  $%.2f / $%.2f  (%s)\n",
		meterColor(pctCost), meterBar(pctCost, barWidth),
		b.EstCostUSD, b.Limits.MaxCost,
		meterFmtPct(pctCost),
	)
	fmt.Printf("  Calls   %s%s\033[0m  %d / %d  (%s)\n",
		meterColor(pctCalls), meterBar(pctCalls, barWidth),
		b.ToolCalls, b.Limits.MaxCalls,
		meterFmtPct(pctCalls),
	)
	if b.Limits.MaxPerMin > 0 {
		fmt.Printf("  Rate    %s%s\033[0m  %d / %d per min  (%s)\n",
			meterColor(pctRate), meterBar(pctRate, barWidth),
			b.CallsLastMin, b.Limits.MaxPerMin,
			meterFmtPct(pctRate),
		)
	}

	// Warnings at 90%.
	if pctTokens >= 0.9 {
		fmt.Printf("\n  \033[31m⚠  TOKEN BUDGET: %.0f%% used — session will be terminated at limit\033[0m\n", pctTokens*100)
	} else if pctCost >= 0.9 {
		fmt.Printf("\n  \033[31m⚠  COST BUDGET: $%.2f of $%.2f — approaching limit\033[0m\n", b.EstCostUSD, b.Limits.MaxCost)
	} else if pctCalls >= 0.9 {
		fmt.Printf("\n  \033[33m⚠  CALL BUDGET: %d of %d tool calls used\033[0m\n", b.ToolCalls, b.Limits.MaxCalls)
	}
}

// ── rendering helpers ─────────────────────────────────────────────────────────

func meterBar(pct float64, width int) string {
	if pct > 1.0 {
		pct = 1.0
	}
	filled := int(float64(width) * pct)
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func meterColor(pct float64) string {
	switch {
	case pct >= 0.9:
		return "\033[31m" // red
	case pct >= 0.7:
		return "\033[33m" // yellow
	default:
		return "\033[32m" // green
	}
}

func meterPct(used, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	return used / limit
}

func meterFmtPct(p float64) string {
	return fmt.Sprintf("%.1f%%", p*100)
}

// meterComma formats an integer with comma thousands separators.
func meterComma(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var out []byte
	start := len(s) % 3
	if start == 0 {
		start = 3
	}
	out = append(out, s[:start]...)
	for i := start; i < len(s); i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}
