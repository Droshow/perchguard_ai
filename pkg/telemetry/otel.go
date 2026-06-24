// Package telemetry wires up OpenTelemetry for PerchGuard.
// Borrowed directly from the EKS-BankingKube Dynamic_Pod_Sec pattern.
// Every admission decision produces a span and audit log entry.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// TracerProvider is a thin wrapper around the OTEL SDK tracer provider.
type TracerProvider struct {
	provider *sdktrace.TracerProvider
}

// InitTracer sets up the OpenTelemetry tracer.
// If OTEL_EXPORTER_OTLP_ENDPOINT is set, spans are exported to that endpoint (Jaeger).
// Otherwise traces are collected in memory only (no-op exporter — local dev without Jaeger).
func InitTracer(serviceName string) (*TracerProvider, error) {
	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("resource init: %w", err)
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
	}

	// Wire OTLP HTTP exporter if endpoint is configured.
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint != "" {
		exp, err := otlptracehttp.New(
			context.Background(),
			otlptracehttp.WithEndpoint(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")),
			otlptracehttp.WithInsecure(), // Jaeger in dev does not use TLS
		)
		if err != nil {
			log.Printf("[perchguard/telemetry] OTLP exporter init failed: %v — falling back to no-op", err)
		} else {
			opts = append(opts, sdktrace.WithBatcher(exp))
			log.Printf("[perchguard/telemetry] OTLP exporter wired → %s", endpoint)
		}
	} else {
		log.Printf("[perchguard/telemetry] no OTEL_EXPORTER_OTLP_ENDPOINT — traces not exported")
	}

	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	return &TracerProvider{provider: tp}, nil
}

func (t *TracerProvider) Shutdown(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := t.provider.Shutdown(ctx); err != nil {
		log.Printf("[perchguard/telemetry] shutdown error: %v", err)
	}
}

// AuditLogger writes admission decisions to stdout (and optionally OTLP).
// Implements the admission.AuditLogger interface.
// Mirrors the OpenTelemetry metric/span pattern from Dynamic_Pod_Sec.
type AuditLogger struct {
	policy    policy.AuditPolicy
	redactSet map[string]struct{}
}

func NewAuditLogger(p policy.AuditPolicy) *AuditLogger {
	redact := make(map[string]struct{}, len(p.RedactedFields))
	for _, f := range p.RedactedFields {
		redact[strings.ToLower(f)] = struct{}{}
	}
	return &AuditLogger{policy: p, redactSet: redact}
}

// Log writes a structured audit entry.
// Format: JSON for easy ingestion by Loki, CloudWatch, Splunk, etc.
func (a *AuditLogger) Log(entry admission.AuditEntry) {
	if !a.policy.Enabled {
		return
	}

	type auditLine struct {
		Level         string   `json:"level"`
		Service       string   `json:"service"`
		RequestUID    string   `json:"request_uid"`
		SessionID     string   `json:"session_id"`
		AgentID       string   `json:"agent_id"`
		Tool          string   `json:"tool"`
		Decision      string   `json:"decision"`
		Policy        string   `json:"policy,omitempty"`
		Reason        string   `json:"reason"`
		DurationMs    int64    `json:"duration_ms"`
		Timestamp     string   `json:"timestamp"`
		PolicyVersion string   `json:"policy_version,omitempty"`
		DataRefsIn    []string `json:"data_refs_in,omitempty"`
		DataRefOut    string   `json:"data_ref_out,omitempty"`
	}

	line := auditLine{
		Level:         "INFO",
		Service:       "perchguard",
		RequestUID:    entry.RequestUID,
		SessionID:     entry.SessionID,
		AgentID:       entry.AgentID,
		Tool:          entry.ToolName,
		Decision:      string(entry.Decision),
		Policy:        entry.PolicyHit,
		Reason:        entry.Reason,
		DurationMs:    entry.DurationMs,
		Timestamp:     entry.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		PolicyVersion: entry.PolicyVersion,
		DataRefsIn:    entry.DataRefsIn,
		DataRefOut:    entry.DataRefOut,
	}

	b, err := json.Marshal(line)
	if err != nil {
		log.Printf("[perchguard/audit] marshal error: %v", err)
		return
	}
	fmt.Println(string(b))
}

// TeeAuditLogger wraps AuditLogger and additionally pushes each entry into an
// AuditRingBuffer so the management API can query recent decisions in memory.
// Implements admission.AuditLogger — drop-in replacement for AuditLogger.
type TeeAuditLogger struct {
	inner     *AuditLogger
	ring      *store.AuditRingBuffer
	versionFn func() string // returns current policy hash; called at log time to capture hot-reload changes
}

// NewTeeAuditLogger creates a TeeAuditLogger. versionFn is called on every Log to stamp
// each AuditRecord with the policy hash that was in effect at decision time.
func NewTeeAuditLogger(inner *AuditLogger, ring *store.AuditRingBuffer, versionFn func() string) *TeeAuditLogger {
	return &TeeAuditLogger{inner: inner, ring: ring, versionFn: versionFn}
}

func (t *TeeAuditLogger) Log(entry admission.AuditEntry) {
	if t.versionFn != nil {
		entry.PolicyVersion = t.versionFn()
	}
	t.inner.Log(entry)

	// Prometheus metrics — no-op if metrics are not registered (tests, early init).
	InterceptTotal.WithLabelValues(string(entry.Decision)).Inc()
	InterceptDuration.Observe(float64(entry.DurationMs) / 1000)
	if entry.RiskScore > 0 {
		SessionRiskScore.Observe(entry.RiskScore)
	}

	// Phase 7 fleet metrics.
	DecisionsTotal.WithLabelValues(entry.SessionID, entry.ToolName, string(entry.Decision)).Inc()
	if entry.RiskScore > 0 {
		RiskScoreGauge.WithLabelValues(entry.SessionID).Set(entry.RiskScore)
	}
	HookDuration.WithLabelValues("total").Observe(float64(entry.DurationMs) / 1000)
	if entry.PolicyHit != "" {
		PolicyViolationsTotal.WithLabelValues(entry.PolicyHit, entry.ToolName).Inc()
	}
	switch admission.Decision(entry.Decision) {
	case admission.DecisionDeny, admission.DecisionTerminate:
		BlockedToolsTotal.WithLabelValues(entry.ToolName, entry.Reason).Inc()
	}

	t.ring.Push(store.AuditRecord{
		RequestUID:    entry.RequestUID,
		SessionID:     entry.SessionID,
		AgentID:       entry.AgentID,
		ToolName:      entry.ToolName,
		Decision:      string(entry.Decision),
		PolicyHit:     entry.PolicyHit,
		Reason:        entry.Reason,
		Timestamp:     entry.Timestamp,
		DurationMs:    entry.DurationMs,
		RiskScore:     entry.RiskScore,
		Registered:    entry.Registered,
		PolicyVersion: entry.PolicyVersion,
		DataRefsIn:    entry.DataRefsIn,
		DataRefOut:    entry.DataRefOut,
		DriftScore:    entry.DriftScore,
	})
}
