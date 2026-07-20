# PerchGuard OpenTelemetry (OTEL) Tracing Implementation

## Overview

PerchGuard uses OpenTelemetry to create distributed traces for every tool call admission decision. Each request produces a hierarchical span structure showing which validators/mutators/quotas ran, their results, and decision outcomes. Spans are exported to Jaeger via OTLP/HTTP for visualization.

---

## 1. How Spans Are Created for Admission Decisions

### Initialization (pkg/telemetry/otel.go)

```go
// InitTracer sets up the OpenTelemetry tracer
// - Reads OTEL_EXPORTER_OTLP_ENDPOINT from environment
// - Sends spans via OTLP/HTTP (insecure, for dev Jaeger)
// - Falls back to no-op exporter if endpoint not configured
func InitTracer(serviceName string) (*TracerProvider, error) {
    res, err := resource.New(context.Background(),
        resource.WithAttributes(semconv.ServiceName(serviceName)),
    )
    // ...
    
    opts := []sdktrace.TracerProviderOption{
        sdktrace.WithSampler(sdktrace.AlwaysSample()),
        sdktrace.WithResource(res),
    }

    endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    if endpoint != "" {
        exp, err := otlptracehttp.New(
            context.Background(),
            otlptracehttp.WithEndpoint(strings.TrimPrefix(..., "http://")),
            otlptracehttp.WithInsecure(),
        )
        opts = append(opts, sdktrace.WithBatcher(exp))
    }
    
    tp := sdktrace.NewTracerProvider(opts...)
    otel.SetTracerProvider(tp)
    return &TracerProvider{provider: tp}, nil
}
```

**Service name**: `"perchguard"`  
**Sampling**: `AlwaysSample()` — all spans exported (production should use probabilistic sampling)  
**Export protocol**: OTLP/HTTP (gRPC also supported but not configured)

---

## 2. Inbound Admission Decision Span Hierarchy

### Root Span: `/intercept` Endpoint

**File**: [pkg/admission/interceptor.go](pkg/admission/interceptor.go#L181)

```go
func (i *Interceptor) Intercept(ctx context.Context, req *ToolCallAdmissionRequest) *ToolCallAdmissionResponse {
    ctx, span := otel.Tracer("perchguard").Start(ctx, "intercept")
    defer span.End()
    
    span.SetAttributes(
        attribute.String("session_id", req.SessionID),
        attribute.String("agent_id", req.AgentID),
        attribute.String("agent_role", req.AgentRole),
        attribute.String("tool", req.ToolCall.Name),
    )
```

### Span Tree Structure

The `intercept` root span contains child spans for each phase of the pipeline:

```
POST /intercept → intercept (root span, duration=<total>)
  │
  ├── quota.<quota_name>           [child span for each quota check]
  │   ├── session_budget           (typically <1ms)
  │   └── depth_limiter            (typically <1ms)
  │
  ├── validator.<validator_name>   [child span for each validator]
  │   ├── prompt_injection         (typically <1ms)
  │   ├── tool_authorization       (typically <1ms)
  │   ├── data_exfiltration        (typically <1ms)
  │   ├── semantic_firewall        (typically 150-200ms, LLM call)
  │   ├── pii_biometric            (typically <1ms)
  │   ├── agent_fleet              (typically <1ms)
  │   └── lineage                  (typically <1ms)
  │
  ├── mutator.<mutator_name>       [child span for each mutator, only if no violations]
  │   ├── parameter_sanitizer      (typically <1ms)
  │   └── least_privilege          (typically <1ms)
  │
  └── [final decision recorded as root span attribute]
```

**Execution order**:
1. Quota checks (fail-fast: stop on first violation)
2. Validators (collect all violations; each gets own span)
3. Mutators (only if no violations; each gets own span)
4. Quotas record (on ALLOW/MUTATE only)

---

## 3. Span Attributes and Status Codes

### Root Span Attributes (set on entry)

```go
span.SetAttributes(
    attribute.String("session_id", req.SessionID),       // e.g., "pg-insurance-001-abc123"
    attribute.String("agent_id", req.AgentID),           // e.g., "insurance-agent-001"
    attribute.String("agent_role", req.AgentRole),       // e.g., "read_only_agent", "admin_agent"
    attribute.String("tool", req.ToolCall.Name),         // e.g., "search_claims", "write_report"
)
```

### Root Span Attributes (set on decision)

```go
span.SetAttributes(attribute.String("decision", string(resp.Decision)))
// decision ∈ { "ALLOW", "DENY", "MUTATE", "HUMAN_REVIEW", "TERMINATE" }

// Status codes for error highlighting in Jaeger
if resp.Decision == DecisionDeny || resp.Decision == DecisionTerminate {
    span.SetStatus(codes.Error, resp.Reason)  // ← renders RED in Jaeger
} else {
    span.SetStatus(codes.Ok, "")              // ← renders GREEN in Jaeger
}
```

### Child Span Status Codes

Each validator/quota/mutator span records its result:

```go
// In Intercept, for each validator:
for _, v := range validators {
    _, vSpan := otel.Tracer("perchguard").Start(ctx, "validator."+v.Name())
    viol := v.Validate(ctx, req)
    if viol != nil {
        vSpan.SetStatus(codes.Error, viol.Detail)  // RED span
        violations = append(violations, *viol)
    }
    vSpan.End()
}

// In Intercept, for each quota:
for _, q := range quotas {
    _, qSpan := otel.Tracer("perchguard").Start(ctx, "quota."+q.Name())
    v := q.Check(ctx, req)
    if v != nil {
        qSpan.SetStatus(codes.Error, v.Detail)  // RED span (stop here)
        // ... return TERMINATE response
    }
    qSpan.End()
}
```

### Span Duration

The root span duration is automatically calculated by the OTEL SDK. Child spans inherit the timing of their execution.

**Example breakdown** (from a semantic firewall call):
- `quota.depth_limiter`: 0ms
- `quota.session_budget`: 0ms
- `validator.prompt_injection`: 0ms
- `validator.tool_authorization`: 0ms
- `validator.semantic_firewall`: 185ms ← LLM API call blocks here
- **Total `intercept` span**: ~190ms

---

## 4. Outbound Validation Span Hierarchy

### Outbound Root Span

**File**: [pkg/admission/interceptor.go](pkg/admission/interceptor.go#L328)

```go
// POST /validate/output
func (i *Interceptor) InterceptOutput(ctx context.Context, req *ToolCallAdmissionRequest) *ToolCallAdmissionResponse {
    ctx, span := otel.Tracer("perchguard").Start(ctx, "intercept_output")
    defer span.End()
    
    span.SetAttributes(
        attribute.String("session_id", req.SessionID),
    )

    // ... outbound validators
    for _, v := range outboundVals {
        _, vSpan := otel.Tracer("perchguard").Start(ctx, "outbound."+v.Name())
        viol := v.Validate(ctx, req)
        if viol != nil {
            vSpan.SetStatus(codes.Error, viol.Detail)
            violations = append(violations, *viol)
        }
        vSpan.End()
    }

    // ... final decision
    span.SetAttributes(attribute.String("decision", string(resp.Decision)))
    if resp.Decision == DecisionDeny || resp.Decision == DecisionTerminate {
        span.SetStatus(codes.Error, resp.Reason)
    }
```

**Outbound validators** (typically PII/biometric scanning):
- `outbound.pii_biometric` — scans output for PII-shaped content
- Any custom sanitizers registered via `WithOutboundValidators()`

---

## 5. Escalation Session Trace Example

### Scenario: Role Escalation Attempt (search_claims → write_report → run_sql)

**Session**: `pg-escalation-test-agent-1234567`  
**Agent role**: `read_only_agent` (allowed: `search_claims`, `read_policy` only)

### Trace Structure in Jaeger

```
┌─ intercept (session_id=pg-escalation-..., tool=search_claims, agent_role=read_only_agent) [GREEN, 195ms]
│  ├─ quota.depth_limiter [OK, 0ms]
│  ├─ quota.session_budget [OK, 0ms]
│  ├─ validator.tool_authorization [OK, 0ms] ✓ search_claims in allow list
│  ├─ validator.data_exfiltration [OK, 0ms]
│  ├─ validator.semantic_firewall [OK, 192ms]
│  ├─ validator.agent_fleet [OK, 0ms]
│  ├─ mutator.parameter_sanitizer [OK, 0ms]
│  └─ decision: ALLOW
│
├─ intercept (session_id=pg-escalation-..., tool=write_report, agent_role=read_only_agent) [RED, 2ms]
│  ├─ quota.depth_limiter [OK, 0ms]
│  ├─ quota.session_budget [OK, 0ms]
│  └─ validator.tool_authorization [ERROR, 1ms] ✗ write_report NOT in allow list
│     ├─ detail: "not in allow list"
│     └─ severity: "high"
│  └─ decision: DENY
│
└─ intercept (session_id=pg-escalation-..., tool=run_sql, agent_role=read_only_agent) [RED, 2ms]
   ├─ quota.depth_limiter [OK, 0ms]
   ├─ quota.session_budget [OK, 0ms]
   └─ validator.tool_authorization [ERROR, 1ms] ✗ run_sql NOT in allow list
      ├─ detail: "not in allow list"
      └─ severity: "high"
   └─ decision: DENY
```

### Timestamps and Linking

Each span has:
- **Start time**: when the span began (microsecond precision)
- **End time**: when the span completed
- **Duration**: automatically calculated
- **Parent span context**: embedded in parent's context (via `ctx`)
- **Trace ID**: shared across all spans in the trace (set by OTEL SDK)
- **Span ID**: unique per span

**Linking across sessions**: Each session creates separate traces. Spans within a single `/intercept` call are linked via the same trace ID. To correlate multiple decisions from the same session, query Jaeger for all spans with matching `session_id` attribute.

---

## 6. How Traces Are Exported to Jaeger

### Export Configuration

**File**: [pkg/telemetry/otel.go](pkg/telemetry/otel.go#L30-L60)

```go
endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
if endpoint != "" {
    exp, err := otlptracehttp.New(
        context.Background(),
        otlptracehttp.WithEndpoint(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")),
        otlptracehttp.WithInsecure(),  // No TLS for dev
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
```

### Exporter Settings

- **Protocol**: OTLP/HTTP (gRPC variant also available but not configured)
- **Batching**: `WithBatcher(exp)` — batches spans and exports periodically (default 5s timeout)
- **TLS**: Disabled (`WithInsecure()`) — Jaeger in dev mode does not require TLS
- **Endpoint URL format**: `http://localhost:4318` or `http://jaeger:4318` (in Docker)

### Environment Variables

```bash
# Set to enable trace export to Jaeger
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318

# Optional: override service name (default: "perchguard")
export OTEL_SERVICE_NAME=perchguard

# Optional: resource attributes (added to all spans)
export OTEL_RESOURCE_ATTRIBUTES=environment=dev,version=1.0.0
```

### Docker Compose Example

```yaml
services:
  perchguard:
    environment:
      OTEL_EXPORTER_OTLP_ENDPOINT: http://jaeger:4318
  
  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "6831:6831/udp"
      - "16686:16686"  # UI
    environment:
      COLLECTOR_OTLP_ENABLED: "true"
```

Access Jaeger UI: `http://localhost:16686`

---

## 7. Trace Structure for a Session

### Complete Session Lifecycle

```
Session: pg-insurance-task3-...

[08:15:30.123] ┌─ intercept (search_claims) [ALLOW, 192ms]
                 ├─ quota.session_budget [OK]
                 ├─ validator.tool_authorization [OK]
                 ├─ validator.semantic_firewall [OK, LLM: 185ms]
                 └─ decision: ALLOW
                 
[08:15:30.316] ├─ intercept (write_report) [DENY, 2ms]
                 ├─ quota.session_budget [OK]
                 ├─ validator.tool_authorization [ERROR: "not in role"]
                 └─ decision: DENY
                 
[08:15:30.319] └─ intercept (run_sql) [DENY, 2ms]
                 ├─ quota.session_budget [OK]
                 ├─ validator.tool_authorization [ERROR: "not in role"]
                 └─ decision: DENY
```

### What Each Span Tells You

| Span | Meaning | Attributes | Decision |
|------|---------|-----------|----------|
| `intercept` | One tool call admission decision | session_id, agent_id, tool, decision | ALLOW/DENY/MUTATE/TERMINATE |
| `quota.*` | Session quota check (budget, depth limit) | (inherits from parent) | OK if check passes, ERROR if violated |
| `validator.*` | One security policy check | (inherits from parent) | OK if no violation, ERROR if violated |
| `mutator.*` | Parameter transformation (only if validators pass) | (inherits from parent) | OK always (errors logged, not span-fatal) |

---

## 8. Decision Outcome Recording

### Policy Violation Recorded as Span Status

When a validator detects a violation:

```go
viol := v.Validate(ctx, req)
if viol != nil {
    vSpan.SetStatus(codes.Error, viol.Detail)
    // viol.Detail example: "not in allow list"
    // viol.Severity: "critical", "high", "medium", "low"
    // viol.Policy: "toolAuthorization", "dataExfiltration", etc.
}
```

### All Violations Collected, Highest Severity Wins

```go
// resolveDecision picks the most severe across all violations
func resolveDecision(violations []PolicyViolation) Decision {
    // Severity mapping: critical/high → DENY, medium/low → HUMAN_REVIEW
    // TERMINATE > DENY > HUMAN_REVIEW > (none)
}
```

### Decision Recorded at Root Level

```go
span.SetAttributes(attribute.String("decision", string(resp.Decision)))

// Color coding in Jaeger:
if resp.Decision == DecisionDeny || resp.Decision == DecisionTerminate {
    span.SetStatus(codes.Error, resp.Reason)      // ← RED
} else {
    span.SetStatus(codes.Ok, "")                  // ← GREEN
}
```

---

## 9. Key Span Attributes Reference

### Root Span Attributes (intercept)

| Attribute | Type | Example | Meaning |
|-----------|------|---------|---------|
| `session_id` | string | `pg-task1-abc123def456` | Session identifier (for grouping decisions from one agent run) |
| `agent_id` | string | `insurance-agent-001` | Agent identifier (which agent made the call) |
| `agent_role` | string | `read_only_agent` | Agent's declared role (used in RBAC policies) |
| `tool` | string | `search_claims` | Name of the tool being called |
| `decision` | string | `ALLOW` | Final decision: ALLOW, DENY, MUTATE, HUMAN_REVIEW, TERMINATE |

### Child Span Names (conventions)

| Span Name Pattern | Meaning |
|---|---|
| `quota.<name>` | Quota checker named `<name>` (e.g., `quota.session_budget`) |
| `validator.<name>` | Validator named `<name>` (e.g., `validator.tool_authorization`) |
| `mutator.<name>` | Mutator named `<name>` (e.g., `mutator.parameter_sanitizer`) |
| `outbound.<name>` | Outbound validator named `<name>` (e.g., `outbound.pii_biometric`) |

---

## 10. Querying Traces in Jaeger

### Finding the Escalation Session

1. Open Jaeger UI: `http://localhost:16686`
2. **Service**: select `perchguard`
3. **Find Traces**:
   - **Session ID**: search for exact session ID (e.g., `pg-escalation-test-agent-1234567`)
   - **Tags**: filter by `tool=write_report` or `decision=DENY`
   - **Span name**: filter by `intercept`

### Trace View

**List view**: Shows all traces (sorted by most recent)
- Trace ID
- Service name
- Duration
- Number of spans
- Error indicator (red = has error spans)

**Detail view** (expand a trace):
- **Timeline**: all spans in chronological order
- **Span list**: hierarchical tree of parent/child spans
- **Span details** (click a span):
  - Span name
  - Duration
  - Start time
  - Attributes (session_id, agent_id, decision, etc.)
  - Status code (OK/Error)
  - Status description (error message if any)

---

## 11. Common Trace Patterns

### ALLOW with Semantic Firewall (Slow Path)

```
intercept (duration: 185-200ms) [GREEN]
├─ quota.depth_limiter (0ms) [OK]
├─ quota.session_budget (0ms) [OK]
├─ validator.prompt_injection (0ms) [OK]
├─ validator.tool_authorization (0ms) [OK]
├─ validator.data_exfiltration (0ms) [OK]
├─ validator.semantic_firewall (180ms) [OK]  ← LLM call here
├─ validator.pii_biometric (0ms) [OK]
├─ validator.agent_fleet (0ms) [OK]
├─ mutator.parameter_sanitizer (0ms) [OK]
├─ mutator.least_privilege (0ms) [OK]
└─ decision: ALLOW
```

### DENY on Policy Violation (Fast Fail)

```
intercept (duration: 1-2ms) [RED]
├─ quota.depth_limiter (0ms) [OK]
├─ quota.session_budget (0ms) [OK]
└─ validator.tool_authorization (1ms) [ERROR: "not in allow list"]
   └── short-circuit: return DENY immediately
```

### MUTATE on PII Detection (Outbound)

```
intercept_output (duration: 5-10ms) [YELLOW]
├─ outbound.pii_biometric (8ms) [OK, but matched pattern]
└─ decision: MUTATE
   └─ sanitized_output: "[REDACTED]"
```

---

## 12. Troubleshooting

### No Traces Appearing in Jaeger

**Check**:
1. `OTEL_EXPORTER_OTLP_ENDPOINT` is set and accessible
   ```bash
   curl http://localhost:4318/
   ```
2. PerchGuard logs show exporter initialization
   ```
   [perchguard/telemetry] OTLP exporter wired → http://jaeger:4318
   ```
3. Jaeger is running and receiver is enabled
   ```bash
   docker logs jaeger | grep "OTEL\|gRPC"
   ```

### Spans Not Showing Expected Decision

**Possible causes**:
- Decision is being overridden in `observe_mode` (audit-only)
- Validator name doesn't match (check [configs/policies.yaml](configs/policies.yaml) for exact names)
- Trace is being sampled out (set `AlwaysSample()` in dev; use probabilistic in production)

### Jaeger Shows Only Root Span, No Children

**Possible causes**:
- Version mismatch: child spans require `otel.Tracer("perchguard").Start(ctx, name)` in a loop
- Child spans are ending but not being flushed (check `otel.SetTracerProvider()` is called)
- Verify code version includes [commit abc123](pkg/admission/interceptor.go#L206) or later

---

## 13. Production Considerations

### Sampling Strategy

**Development** (current):
```go
sdktrace.WithSampler(sdktrace.AlwaysSample())  // Export all spans
```

**Production** (recommended):
```go
// Export ~10% of traces (configurable per-span)
sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1))
```

### Exporter Batching

Batching is automatic via `WithBatcher(exp)`:
- **Default**: batch timeout 5 seconds, queue size 1024 spans
- **Tuning**: override via exporter config if needed

### Retention

Jaeger's default retention:
- **Cassandra backend** (production): 72 hours
- **In-memory backend** (dev): only while running

### Metrics vs Traces

PerchGuard currently exports **traces only**. Metrics (`ALLOW_count`, `DENY_count`, `latency_p99`) are available via:
- Audit log lines (queryable via `GET /api/audit`)
- Jaeger span counts (aggregated by decision attribute)
- Future: Prometheus `/metrics` endpoint

---

## 14. Files and References

| File | Purpose |
|------|---------|
| [pkg/telemetry/otel.go](pkg/telemetry/otel.go) | Tracer initialization, OTLP exporter setup |
| [pkg/admission/interceptor.go](pkg/admission/interceptor.go#L181) | Root span creation and child span loops |
| [configs/policies.yaml](configs/policies.yaml) | Policy configuration (validators, quotas, mutators) |
| [cmd/main.go](cmd/main.go#L202) | `telemetry.InitTracer()` call |
| [go.mod](go.mod) | OTEL dependencies: `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` |

---

## 15. Example: Manual Trace Query

### cURL to PerchGuard → Jaeger Query

```bash
# Make a tool call admission request
curl -X POST http://localhost:8080/intercept \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "test-session-001",
    "agent_id": "test-agent",
    "agent_role": "read_only_agent",
    "tool_call": {
      "name": "search_claims",
      "parameters": {"query": "test"}
    }
  }'

# Get the response decision
# → decision: ALLOW

# Query Jaeger for the trace
# Go to: http://localhost:16686
# → Service: perchguard
# → Filter tags: session_id=test-session-001, decision=ALLOW
# → Find the trace with ~8 child spans (7 validators + root)
```

---

## Appendix: Full Span Breakdown for `search_claims` ALLOW

**Timing** (example):
- `quota.depth_limiter`: 0.1ms
- `quota.session_budget`: 0.2ms
- `validator.prompt_injection`: 0.5ms
- `validator.tool_authorization`: 0.3ms
- `validator.data_exfiltration`: 0.4ms
- `validator.semantic_firewall`: 185.0ms (LLM API call)
- `validator.pii_biometric`: 0.6ms
- `validator.agent_fleet`: 0.7ms
- `mutator.parameter_sanitizer`: 0.4ms
- `mutator.least_privilege`: 0.3ms
- **Total**: ~188ms

**Attributes** (root span):
```json
{
  "session_id": "pg-insurance-task1-5718e4f76b51",
  "agent_id": "insurance-agent-001",
  "agent_role": "read_only_agent",
  "tool": "search_claims",
  "decision": "ALLOW"
}
```

**Status**: `Ok` (GREEN in Jaeger)
