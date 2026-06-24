# Contributing to PerchGuard

---

## Development setup

**Requirements:** Go 1.22+, Docker (for integration tests), `make` optional.

```bash
git clone https://github.com/perchguard/PerchGuard_AI.git
cd PerchGuard_AI/perchguard

# Build
go build ./...

# Test suite
go test ./...

# Race detector — run before every PR
go test -race ./...

# Run locally
go run ./cmd
# Listens on :8080, API key printed to stdout
```

Optional: copy `.env.example` to `.env` and set `ANTHROPIC_API_KEY_USED_BY_PERCHGUARD` to enable the semantic firewall in local development.

---

## Repository layout

```
cmd/
  main.go           — entry point; wires all packages
  watch.go          — --mode=watch terminal dashboard
  redteam-agent/    — adversarial test harness

pkg/
  admission/        — core pipeline: Interceptor, types, Validator/Mutator/QuotaChecker interfaces
  admission/validator/  — one file per validator
  admission/mutator/    — one file per mutator
  admission/quota/      — one file per quota checker
  agent/            — fleet manager, session state, attack chain detector
  api/              — management REST API handlers
  audit/            — audit sinks (JSONL, stdout, OTLP)
  llm/              — Claude API client
  localfs/          — local context/snapshot reader-writer
  manifest/         — agent manifest store (declared intent)
  mcp/              — MCP proxy
  policy/           — YAML loader, hot-reload watcher
  store/            — session store, audit ring buffer, lineage store
  telemetry/        — OpenTelemetry tracer + Prometheus metrics

configs/
  policies.yaml     — default policy (enforcement: observe)

deployments/
  docker-compose/   — local dev stack
  helm/perchguard/  — Helm chart
  eks/              — Terraform EKS Fargate deployment
  k3s/              — k3d/k3s manifests

artifacts/docs/     — operator documentation, design documents, and phase specs
snapshots/          — runtime output (gitignored except governance.json)
```

---

## Adding a validator

Validators implement `admission.Validator`:

```go
type Validator interface {
    Name() string
    Validate(ctx context.Context, req *ToolCallAdmissionRequest) *PolicyViolation
}
```

Return `nil` to allow. Return a `*PolicyViolation` to flag a problem:

```go
return &admission.PolicyViolation{
    Policy:   "myValidator",
    Severity: admission.SeverityHigh,
    Detail:   "reason for the operator",
    Decision: admission.DecisionDeny,
}
```

Steps:
1. Create `pkg/admission/validator/my_validator.go`
2. Add a config struct to `pkg/policy/loader.go` under `Policies`
3. Add the corresponding YAML fields to `configs/policies.yaml`
4. Wire it in `cmd/main.go` `buildPipelineSlices()`
5. Write a test in `pkg/admission/validator/my_validator_test.go`

The interceptor calls validators in order; all violations are collected before deciding. The most severe decision wins (`TERMINATE > DENY > HUMAN_REVIEW`).

---

## Adding a mutator

Mutators implement `admission.Mutator`:

```go
type Mutator interface {
    Name() string
    Mutate(ctx context.Context, req *ToolCallAdmissionRequest) (*ToolCall, error)
}
```

Return the modified `*ToolCall` or `nil` to leave it unchanged. Wire it in `buildPipelineSlices()`.

---

## Pull request checklist

Before opening a PR:

- [ ] `go test -race ./...` passes
- [ ] Run `/security-review` (Claude Code skill) and verify `SECURITY_CHECKLIST.md`
- [ ] Token/key comparisons use `crypto/subtle.ConstantTimeCompare`
- [ ] Random generation uses `crypto/rand`
- [ ] New HTTP request bodies are wrapped with `io.LimitReader`
- [ ] New `/api/*` routes go through `requireAPIKey` in `RegisterRoutes`
- [ ] Error responses do not include internal paths, stack traces, or error details

---

## Commit style

```
feat(scope): short description

Longer explanation if needed. Focus on WHY, not what.

Co-Authored-By: ...
```

Scope examples: `admission`, `api`, `policy`, `telemetry`, `eks`, `helm`, `phase6`.

---

## Running the integration smoke test

The declared-intent lab is the canonical integration test:

```bash
# Start PerchGuard with a known API key (DELETE /api/sessions is key-guarded)
export PERCHGUARD_API_KEY=lab-key
go run ./cmd &
PERCHGUARD_PID=$!
sleep 1

# Register an agent
SESSION=$(curl -s -X POST http://localhost:8080/agents/register \
  -H "Content-Type: application/json" \
  -d '{
    "apiVersion": "perchguard/v1", "kind": "AgentManifest",
    "metadata": {"id": "lab", "version": "0.1.0"},
    "mission": {"summary": "test", "scope": ["read_file"]},
    "authorization": {"role": "read_only_agent"}
  }' \
  | jq -r .session_id)

# Fire a call
curl -s -X POST http://localhost:8080/intercept \
  -H "Content-Type: application/json" \
  -d "{\"uid\":\"1\",\"session_id\":\"$SESSION\",\"agent_id\":\"lab\",\"agent_role\":\"read_only_agent\",\"tool_call\":{\"name\":\"read_file\",\"parameters\":{}}}" \
  | jq .decision

# Delete session → emits governance record
curl -s -X DELETE http://localhost:8080/api/sessions/$SESSION \
  -H "Authorization: Bearer $PERCHGUARD_API_KEY"

# Read governance snapshot
cat snapshots/governance.json | jq .

kill $PERCHGUARD_PID
```

Expected: decision `ALLOW`, governance record in `snapshots/governance.json`.

For the full walkthrough — including a denied call, a human-review call carrying
PII, and the compliance-report export — see `scripts/demo.sh` and
[artifacts/docs/demo.md](artifacts/docs/demo.md).
