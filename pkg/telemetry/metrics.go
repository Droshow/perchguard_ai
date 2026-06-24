package telemetry

import "github.com/prometheus/client_golang/prometheus"

// Intercept pipeline counters and histograms.
var (
	InterceptTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "perchguard_intercept_total",
			Help: "Total admission decisions, partitioned by decision type.",
		},
		[]string{"decision"},
	)

	InterceptDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "perchguard_intercept_duration_seconds",
		Help:    "Admission pipeline latency in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	SemanticFirewallDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "perchguard_semantic_firewall_duration_seconds",
		Help:    "LLM call latency for the semantic firewall validator.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
	})

	SessionRiskScore = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "perchguard_session_risk_score",
		Help:    "Distribution of cumulative session risk scores at decision time.",
		Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
	})

	PolicyReloadTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "perchguard_policy_reload_total",
		Help: "Total successful policy hot-reloads.",
	})

	// Phase 7 — fleet-level metrics for Grafana fleet dashboard.

	DecisionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "perchguard_decisions_total",
			Help: "Total admission decisions labelled by session, tool, and outcome.",
		},
		[]string{"session", "tool", "decision"},
	)

	RiskScoreGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "perchguard_risk_score",
			Help: "Current cumulative risk score per session.",
		},
		[]string{"session_id"},
	)

	TokensUsedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "perchguard_tokens_used_total",
			Help: "Total tokens consumed, labelled by session and model.",
		},
		[]string{"session", "model"},
	)

	HookDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "perchguard_hook_duration_seconds",
			Help:    "Admission pipeline duration per stage.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		},
		[]string{"pipeline_stage"},
	)

	PolicyViolationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "perchguard_policy_violations_total",
			Help: "Total policy violations, labelled by policy name and tool.",
		},
		[]string{"policy_name", "tool"},
	)

	BlockedToolsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "perchguard_blocked_tools_total",
			Help: "Total blocked tool calls, labelled by tool name and reason.",
		},
		[]string{"tool", "reason"},
	)

	PendingReviewsGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "perchguard_pending_reviews",
		Help: "Number of tool calls currently awaiting human review.",
	})
)

// Register adds all static PerchGuard metrics to the default Prometheus registry.
// Call once at startup before serving /metrics.
func Register() {
	prometheus.MustRegister(
		InterceptTotal,
		InterceptDuration,
		SemanticFirewallDuration,
		SessionRiskScore,
		PolicyReloadTotal,
		DecisionsTotal,
		RiskScoreGauge,
		TokensUsedTotal,
		HookDuration,
		PolicyViolationsTotal,
		BlockedToolsTotal,
		PendingReviewsGauge,
	)
}

// RegisterLiveMetrics adds GaugeFunc metrics backed by live data sources.
// activeSessions returns the current count of active agent sessions.
// auditRingLen returns the number of records currently in the audit ring buffer.
func RegisterLiveMetrics(activeSessions func() float64, auditRingLen func() float64) {
	prometheus.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "perchguard_active_sessions",
			Help: "Number of currently active agent sessions.",
		}, activeSessions),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "perchguard_audit_ring_utilization",
			Help: "Number of records currently stored in the audit ring buffer.",
		}, auditRingLen),
	)
}
