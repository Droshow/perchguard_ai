package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// watchConfig holds the parameters for watch mode.
type watchConfig struct {
	addr     string
	apiKey   string
	interval time.Duration
}

// runWatch polls /api/fleet/summary and /api/stats on a fixed interval and
// renders a compact terminal dashboard. No external dependencies — pure stdlib.
func runWatch(cfg watchConfig) {
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	// First tick immediately.
	render(client, cfg)
	for range ticker.C {
		render(client, cfg)
	}
}

// ── API response shapes (subset of server types, duplicated to keep watch
//    self-contained — no import of pkg/api needed) ───────────────────────

type watchFleetSummary struct {
	ActiveSessions   int `json:"active_sessions"`
	HighRiskSessions int `json:"high_risk_sessions"`
	RiskDistribution struct {
		Low    int `json:"low"`
		Medium int `json:"medium"`
		High   int `json:"high"`
	} `json:"risk_distribution"`
	RecentTerminates []struct {
		SessionID string    `json:"session_id"`
		Reason    string    `json:"reason"`
		RiskScore float64   `json:"risk_score"`
		At        time.Time `json:"at"`
	} `json:"recent_terminates"`
	DelegatedSessions  int       `json:"delegated_sessions"`
	MaxDelegationDepth int       `json:"max_delegation_depth"`
	ObservedAt         time.Time `json:"observed_at"`
}

type watchStats struct {
	TotalSessions  int            `json:"total_sessions"`
	Decisions      map[string]int `json:"decisions"`
	TopDeniedTools []struct {
		Tool  string `json:"tool"`
		Count int    `json:"count"`
	} `json:"top_denied_tools"`
	PolicyVersion string `json:"policy_version"`
}

// ── rendering ────────────────────────────────────────────────────────────────

func render(client *http.Client, cfg watchConfig) {
	fleet, fleetErr := fetchFleet(client, cfg)
	stats, statsErr := fetchStats(client, cfg)

	// Clear screen + move cursor to top (works on any ANSI terminal).
	fmt.Print("\033[2J\033[H")

	now := time.Now().Format("2006-01-02 15:04:05")
	policy := "—"
	if statsErr == nil {
		policy = stats.PolicyVersion
	}
	fmt.Printf("PerchGuard Watch  [%s]  policy: %s  interval: %s\n",
		now, policy, cfg.interval)
	fmt.Println(strings.Repeat("─", 72))

	if fleetErr != nil || statsErr != nil {
		fmt.Printf("\n  ✗ cannot reach %s\n", cfg.addr)
		if fleetErr != nil {
			fmt.Printf("    fleet:  %v\n", fleetErr)
		}
		if statsErr != nil {
			fmt.Printf("    stats:  %v\n", statsErr)
		}
		fmt.Printf("\n  Is PerchGuard running? Is PERCHGUARD_API_KEY correct?\n")
		fmt.Println(strings.Repeat("─", 72))
		fmt.Println("Press Ctrl-C to exit")
		return
	}

	// Sessions
	fmt.Printf("\nSESSIONS   %d active", fleet.ActiveSessions)
	if fleet.HighRiskSessions > 0 {
		fmt.Printf("   \033[31m%d high-risk\033[0m", fleet.HighRiskSessions)
	}
	fmt.Printf("   [low: %d  med: %d  high: %d]",
		fleet.RiskDistribution.Low,
		fleet.RiskDistribution.Medium,
		fleet.RiskDistribution.High)
	if fleet.DelegatedSessions > 0 {
		fmt.Printf("   delegated: %d (max depth %d)",
			fleet.DelegatedSessions, fleet.MaxDelegationDepth)
	}
	fmt.Println()

	// Decisions
	fmt.Println("\nDECISIONS (last 1000)")
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	order := []string{"ALLOW", "DENY", "MUTATE", "HUMAN_REVIEW", "TERMINATE"}
	for _, d := range order {
		if n, ok := stats.Decisions[d]; ok {
			color, reset := decisionColor(d)
			fmt.Fprintf(tw, "  %s%-14s%s\t%d\n", color, d, reset, n)
		}
	}
	tw.Flush()

	// Top blocked tools
	if len(stats.TopDeniedTools) > 0 {
		fmt.Println("\nTOP BLOCKED TOOLS")
		tw2 := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, t := range stats.TopDeniedTools {
			fmt.Fprintf(tw2, "  %-30s\t%d\n", truncate(t.Tool, 30), t.Count)
		}
		tw2.Flush()
	}

	// Recent terminates
	if len(fleet.RecentTerminates) > 0 {
		fmt.Println("\nRECENT TERMINATES")
		tw3 := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		limit := 5
		if len(fleet.RecentTerminates) < limit {
			limit = len(fleet.RecentTerminates)
		}
		for _, ev := range fleet.RecentTerminates[:limit] {
			fmt.Fprintf(tw3, "  \033[31m%s\033[0m\trisk=%.2f\t%s\t%s\n",
				truncate(ev.SessionID, 16),
				ev.RiskScore,
				truncate(ev.Reason, 40),
				ev.At.Local().Format("15:04:05"),
			)
		}
		tw3.Flush()
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 72))
	fmt.Println("Press Ctrl-C to exit")
}

func fetchFleet(client *http.Client, cfg watchConfig) (*watchFleetSummary, error) {
	var v watchFleetSummary
	if err := apiGet(client, cfg, "/api/fleet/summary", &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func fetchStats(client *http.Client, cfg watchConfig) (*watchStats, error) {
	var v watchStats
	if err := apiGet(client, cfg, "/api/stats", &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func apiGet(client *http.Client, cfg watchConfig, path string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, cfg.addr+path, nil)
	if err != nil {
		return err
	}
	if cfg.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, dst)
}

func decisionColor(d string) (string, string) {
	switch d {
	case "DENY", "TERMINATE":
		return "\033[31m", "\033[0m" // red
	case "HUMAN_REVIEW":
		return "\033[33m", "\033[0m" // yellow
	case "MUTATE":
		return "\033[36m", "\033[0m" // cyan
	default:
		return "", ""
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
