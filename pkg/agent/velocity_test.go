package agent

import (
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

func velocityCfg(windowMins, threshold int, maxDrift, contribution float64) AgentFleetConfig {
	return AgentFleetConfig{
		VelocityAnomaly: VelocityAnomalyConfig{
			WindowMinutes:    windowMins,
			ThresholdCalls:   threshold,
			MaxAvgDrift:      maxDrift,
			RiskContribution: contribution,
		},
	}
}

func makeEvents(count int, driftScore float64, age time.Duration) []store.ToolEvent {
	events := make([]store.ToolEvent, count)
	now := time.Now()
	for i := range events {
		events[i] = store.ToolEvent{
			Tool:       "read_file",
			Stage:      "recon",
			Timestamp:  now.Add(-age),
			DriftScore: driftScore,
		}
	}
	return events
}

func TestVelocityContribution_Disabled(t *testing.T) {
	a := &SessionAgent{cfg: velocityCfg(0, 0, 0.3, 0.15)} // zero config = disabled
	if c := a.velocityContribution(makeEvents(50, 0.1, 0)); c != 0 {
		t.Errorf("want 0 when config is zeroed, got %.2f", c)
	}
}

func TestVelocityContribution_BelowThreshold(t *testing.T) {
	a := &SessionAgent{cfg: velocityCfg(5, 20, 0.3, 0.15)}
	events := makeEvents(15, 0.1, 30*time.Second) // 15 < threshold of 20
	if c := a.velocityContribution(events); c != 0 {
		t.Errorf("want 0 below threshold, got %.2f", c)
	}
}

func TestVelocityContribution_HighDrift_NoSignal(t *testing.T) {
	a := &SessionAgent{cfg: velocityCfg(5, 20, 0.3, 0.15)}
	// 25 calls in window but avg drift 0.6 > maxAvgDrift 0.3 = diverse = no probe
	events := makeEvents(25, 0.6, 30*time.Second)
	if c := a.velocityContribution(events); c != 0 {
		t.Errorf("want 0 for diverse calls (high drift), got %.2f", c)
	}
}

func TestVelocityContribution_HighVelocityLowDrift_Fires(t *testing.T) {
	a := &SessionAgent{cfg: velocityCfg(5, 20, 0.3, 0.15)}
	// 25 calls with low drift = probe pattern
	events := makeEvents(25, 0.1, 30*time.Second)
	c := a.velocityContribution(events)
	if c != 0.15 {
		t.Errorf("want contribution 0.15, got %.2f", c)
	}
}

func TestVelocityContribution_OncePerWindow(t *testing.T) {
	a := &SessionAgent{cfg: velocityCfg(5, 20, 0.3, 0.15)}
	events := makeEvents(25, 0.1, 30*time.Second)

	first := a.velocityContribution(events)
	if first != 0.15 {
		t.Fatalf("first fire: want 0.15, got %.2f", first)
	}

	// Second call immediately after — one-fire guard should suppress it.
	second := a.velocityContribution(events)
	if second != 0 {
		t.Errorf("second fire within window: want 0 (one-fire guard), got %.2f", second)
	}
}

func TestVelocityContribution_OldEventsIgnored(t *testing.T) {
	a := &SessionAgent{cfg: velocityCfg(5, 20, 0.3, 0.15)}
	// 25 events but all older than the 5-minute window
	events := makeEvents(25, 0.1, 10*time.Minute)
	if c := a.velocityContribution(events); c != 0 {
		t.Errorf("want 0 when all events are outside window, got %.2f", c)
	}
}

func TestVelocityContribution_DefaultMaxDrift(t *testing.T) {
	// MaxAvgDrift=0 should fall back to velocityLowDiversityDefault (0.3)
	a := &SessionAgent{cfg: velocityCfg(5, 20, 0, 0.15)}
	events := makeEvents(25, 0.1, 30*time.Second) // drift 0.1 < default 0.3
	if c := a.velocityContribution(events); c != 0.15 {
		t.Errorf("want 0.15 with zero MaxAvgDrift falling back to default, got %.2f", c)
	}
}
