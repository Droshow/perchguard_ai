# Configuration Reference

PerchGuard is configured by two things: environment variables (server behaviour) and `configs/policies.yaml` (admission policy). The policy file hot-reloads every 30 seconds — no restart needed.

---

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `PERCHGUARD_ADDR` | `:8080` | Listen address. Use `:8443` with TLS. |
| `PERCHGUARD_POLICY` | `./configs/policies.yaml` | Policy file path. |
| `PERCHGUARD_API_KEY` | auto-generated | Management API bearer key. Printed once at startup if not set. |
| `PERCHGUARD_LLM_API_KEY` | — | Anthropic API key. Enables the semantic firewall validator. Without it the semantic firewall is skipped (fail-open). |
| `PERCHGUARD_LLM_MODEL` | `claude-haiku-4-5` | Model for the semantic firewall. |
| `PERCHGUARD_TLS_CERT` | — | Path to TLS certificate. Enables HTTPS when set alongside `_KEY`. |
| `PERCHGUARD_TLS_KEY` | — | Path to TLS private key. |
| `PERCHGUARD_ENV` | — | Set to `production` to enable the startup TLS warning. |
| `PERCHGUARD_CONTEXT_ROOT` | — | Local directory root for governance snapshots and context enrichment. Defaults to `./snapshots`. |
| `PERCHGUARD_MCP_UPSTREAM` | — | Upstream MCP server URL. Required in `--mode=mcp-proxy`. |
| `PERCHGUARD_WATCH_ADDR` | `http://localhost:8080` | Server address for `--mode=watch`. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP endpoint for trace export (e.g. `http://jaeger:4318`). Omit to disable. |

---

## Policy File (`configs/policies.yaml`)

### Top-level

```yaml
enforcement: observe   # "observe" | "enforce"
```

| Value | Behaviour |
|-------|-----------|
| `observe` | Full pipeline runs. Every non-ALLOW decision is overridden to ALLOW. The real decision is recorded in `observed_decision` in the audit trail. **Default on public release.** |
| `enforce` | Normal blocking behaviour. DENY returns HTTP 403. |

---

### `promptInjection`

Detects indirect prompt injection attempts in tool parameters and user intent.

```yaml
promptInjection:
  enabled: true
  suspiciousPatterns:
    - "ignore previous instructions"
    - "you are now"
  scanFields:
    - user_intent
    - tool_parameters
    - tool_output
  action: DENY
```

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Toggle the validator. |
| `suspiciousPatterns` | `[]string` | Substrings that trigger a violation (case-insensitive). |
| `scanFields` | `[]string` | Which request fields to scan. Valid values: `user_intent`, `tool_parameters`, `tool_output`. |
| `action` | string | `DENY` \| `HUMAN_REVIEW`. |

---

### `semanticFirewall`

Calls an LLM to verify that the proposed tool call aligns with the agent's declared mission. Requires `PERCHGUARD_LLM_API_KEY`.

```yaml
semanticFirewall:
  enabled: true
  intentAlignmentThreshold: 0.6
  action: HUMAN_REVIEW
  llm:
    model: "claude-haiku-4-5"
    maxTokens: 300
    temperature: 0.0
    budgetMs: 10000
    skipIfNoIntent: true
```

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Toggle. No-op if `PERCHGUARD_LLM_API_KEY` is not set. |
| `intentAlignmentThreshold` | float | Confidence below this triggers a violation. Range 0–1. Recommended: 0.6. |
| `action` | string | `HUMAN_REVIEW` \| `DENY`. |
| `llm.model` | string | Claude model to use for alignment scoring. |
| `llm.maxTokens` | int | Max tokens in the scoring response. 300 is sufficient. |
| `llm.budgetMs` | int | LLM call timeout in milliseconds. Exceeding this fails open (ALLOW). |
| `llm.skipIfNoIntent` | bool | Skip the firewall when `user_intent` is empty. Recommended `true`. |

---

### `toolAuthorization`

Allow/deny lists per agent role. Wildcards supported: `bash:*` matches all bash subcommands.

```yaml
toolAuthorization:
  enabled: true
  agentRoles:
    - role: "read_only_agent"
      allowedTools: ["read_file", "list_directory"]
      deniedTools:  ["bash", "write_file"]
  action: DENY
```

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Toggle. |
| `agentRoles` | list | Per-role allow/deny lists. Matched by `agent_role` in the admission request. |
| `agentRoles[].role` | string | Role name. Must match `agent_role` in `/intercept` requests exactly. |
| `agentRoles[].allowedTools` | `[]string` | Whitelist. Wildcards: `bash:*`, `database:*`. |
| `agentRoles[].deniedTools` | `[]string` | Blacklist. Evaluated after allowlist. Explicit deny wins. |
| `action` | string | `DENY` \| `HUMAN_REVIEW`. |

---

### `dataExfiltration`

Blocks outbound tool calls to untrusted destinations.

```yaml
dataExfiltration:
  enabled: true
  allowedDestinations: ["*.internal", "api.github.com"]
  blockedDestinations: ["*.ngrok.io", "pastebin.com"]
  blockUnknownDestinations: false
  action: DENY
```

| Field | Type | Description |
|-------|------|-------------|
| `allowedDestinations` | `[]string` | Glob patterns. Calls to these pass. |
| `blockedDestinations` | `[]string` | Glob patterns. Calls to these are denied regardless of allow list. |
| `blockUnknownDestinations` | bool | `true` = allowlist-only mode. Any destination not in `allowedDestinations` is denied. |

---

### `parameterSanitization`

Mutates tool parameters before execution — injects `--dry-run`, strips dangerous flags, enforces read-only.

```yaml
parameterSanitization:
  enabled: true
  rules:
    - match: "bash"
      containsAny: ["rm ", "drop "]
      inject: "--dry-run"
      unless: "dry_run_confirmed=true"
    - match: "bash"
      stripFlags: ["-rf", "--force"]
    - match: "read_file"
      enforceReadOnly: true
```

| Field | Type | Description |
|-------|------|-------------|
| `match` | string | Tool name to match. |
| `containsAny` | `[]string` | Trigger condition — any of these substrings present in parameters. |
| `inject` | string | String to append to the command parameter. |
| `unless` | string | Session flag that disables the injection. |
| `stripFlags` | `[]string` | Flags to remove from the command parameter. |
| `enforceReadOnly` | bool | Forces the tool's `mode` or `write` parameter to read-only. |

---

### `leastPrivilege`

Automatically scopes SQL queries and blocks path traversal.

```yaml
leastPrivilege:
  enabled: true
  sqlInjection:
    autoAppendWhereClause: true
    userContextField: "session.user_id"
  fileOperations:
    allowedBasePaths: ["/workspace", "/tmp/agent_scratch"]
    blockAbsolutePaths: true
    blockPathTraversal: true
```

---

### `sessionBudget`

Quotas per agent session. Exceeding any limit terminates the session.

```yaml
sessionBudget:
  enabled: true
  delegationFraction: 0.5
  limits:
    maxTokensPerSession: 500000
    maxCostPerSessionUSD: 5.00
    maxToolCallsPerSession: 200
    maxToolCallsPerMinute: 30
    idleTimeoutMinutes: 15
    maxDurationMinutes: 120
  anomaly:
    velocityWindowMinutes: 5
    velocityThresholdCalls: 20
    velocityMaxAvgDrift: 0.3
    velocityRiskContribution: 0.15
  action: TERMINATE_SESSION
```

| Field | Description |
|-------|-------------|
| `delegationFraction` | Child (sub-agent) sessions inherit this fraction of the parent's `maxToolCallsPerSession`. 0.5 = child gets 50%. |
| `limits.idleTimeoutMinutes` | Evict session if no tool call arrives within this window. 0 = disabled. |
| `limits.maxDurationMinutes` | Hard cap on session age. 0 = disabled. |
| `anomaly.velocityThresholdCalls` | Calls in `velocityWindowMinutes` needed to trigger probe detection. |
| `anomaly.velocityMaxAvgDrift` | Average semantic drift below this = repetitive / probe-like. |
| `anomaly.velocityRiskContribution` | Risk score added to the session accumulator when probe pattern fires. |

---

### `depthLimiter`

Prevents runaway agent-calling-agent recursion.

```yaml
depthLimiter:
  enabled: true
  maxAgentNestingDepth: 5
  maxParallelAgents: 10
  maxSubtaskChainLength: 20
  action: DENY
```

---

### `outputValidation`

Scans tool outputs for prompt injection and oversized responses.

```yaml
outputValidation:
  enabled: true
  scanToolOutputs: true
  suspiciousOutputPatterns:
    - "SYSTEM:"
    - "ignore all previous"
  maxOutputSizeBytes: 1048576
  action: SANITIZE
  sanitizeMode: "redact"    # "strip" | "redact" | "block"
  redactReplacement: "[REDACTED]"
```

---

### `humanReview`

Dispatches calls for human approval via webhook before deciding.

```yaml
humanReview:
  enabled: true
  webhookURL: "https://your-review-service/review"
  timeoutSeconds: 60
  autoDenyOnTimeout: true
```

If `webhookURL` is empty, `HUMAN_REVIEW` decisions fall back to `DENY`.

---

### `agentFleet`

Session-stateful risk accumulation and attack chain detection.

```yaml
agentFleet:
  enabled: true
  driftThreshold: 0.6
  behaviorWindowSize: 10
  attackChainEnabled: true
```

| Field | Description |
|-------|-------------|
| `driftThreshold` | Cosine semantic drift above this contributes to session risk. |
| `behaviorWindowSize` | Recent tool calls scanned for attack chain patterns. |
| `attackChainEnabled` | Detects `recon → exploit → exfiltrate` sequences across calls. |

---

### `audit`

```yaml
audit:
  enabled: true
  logLevel: "info"          # "debug" | "info" | "warn"
  logAllDecisions: true
  logToolParameters: true
  redactedFields:
    - "password"
    - "secret"
    - "api_key"
    - "token"
    - "authorization"
  exporters:
    - type: "stdout"
    # - type: "otlp"
    #   endpoint: "http://otel-collector:4318"
```

`redactedFields` are matched case-insensitively against parameter key names. Matching values are replaced with `[REDACTED]` in all audit output.
