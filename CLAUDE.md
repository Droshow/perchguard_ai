# PerchGuard — Claude Code Instructions

## What this project is

PerchGuard is an agentic admission controller — a policy enforcement layer that intercepts every tool call an AI agent makes and applies governance rules before the call executes. It is the trust anchor for every agent it governs. If PerchGuard can be spoofed, bypassed, or compromised, the entire governance model collapses.

## Security checklist — mandatory before every branch close

**Before closing any branch or opening a PR, run `/security-review` and verify every item in `SECURITY_CHECKLIST.md`.**

This is not optional. Key items that have burned us before:

- Token comparisons must use `crypto/subtle.ConstantTimeCompare` — plain `==` is a timing attack
- All `/api/*` routes must go through `requireAPIKey` middleware — check `RegisterRoutes` in `pkg/api/server.go`
- All random generation must use `crypto/rand` — never `math/rand`

Run the race detector before every delivery: `go test -race ./...`

## The system

Three habits. Three automated gates. An agent lookup table.

**Habits — do these every session:**
1. **Frame first.** Start every request: "[file:line] + [what I just did] + [what I want]." Without the anchor, Claude solves the wrong problem.
2. **Plan before code.** Non-trivial task (>1 file or any design decision) → say "plan first." Read it. Approve it. Then implement.
3. **Read the diff.** After implementation: read the actual changed lines, not the summary.

**Gates — hooks fire automatically:**
- Post-edit `go vet` runs after every file change
- Pre-commit reminder fires on `git commit`
- Pre-push security gate fires on `git push`

**Agent lookup:**

| When | What |
|------|------|
| Before any design or architecture decision | Challenger — **new session** |
| AI just wrote >50 lines | `/code-review` |
| Change touches auth, crypto, or agent input | `/security-review` |
| Feature landed, about to start next phase | `/simplify` |
| Behavior hard to verify from code | `/verify` |
| Before every merge | `/code-review` minimum |
| Before merge with security surface | `/code-review ultra` + `/security-review` |

See `.claude/AGENTS.md` for full playbook. See `.claude/ai-operator.md` for context formula and session reset guidance.

## Architecture

```
POST /intercept
    └── Interceptor.ServeHTTP
            ├── Token verification (X-PerchGuard-Agent-Token)
            ├── Quota checks     (pkg/admission/quota/)
            ├── Validators       (pkg/admission/validator/)
            │     ├── PromptInjectionValidator
            │     ├── ToolAuthorizationValidator
            │     ├── DataExfiltrationValidator
            │     ├── SemanticFirewallValidator  (needs PERCHGUARD_LLM_API_KEY)
            │     └── FleetManager              (pkg/agent/ — session-stateful)
            ├── Mutators         (pkg/admission/mutator/)
            └── audit() → TeeAuditLogger → stdout + AuditRingBuffer
```

Management API (`/api/*`) requires `Authorization: Bearer <PERCHGUARD_API_KEY>`.
Agent endpoints (`/intercept`, `/validate/output`, `/agents/register`) are unauthenticated by design.

## Non-goals / do not touch

- Do not refactor surrounding code while fixing a targeted bug.
- Do not add abstractions, error handling, or features beyond what the task requires.
- Do not change the audit log schema without considering existing consumers.
- Do not add external dependencies without explicit approval.

## Phase status

| Phase | Name | Status |
|-------|------|--------|
| 1–2 | Per-call admission pipeline | Shipped |
| 3 | Agent fleet (session-stateful) | Shipped |
| 4 | Operability platform + auth | Shipped |
| 5 | Adversarial validation | Shipped |
| 6 | Open source polish | Shipped |
| 7 | Productization | In progress |
| 8 | Agentic compliance verticals (PSD2/Open Banking, EU AI Act, SOC2) | Not started |
| 9 | Session cost telemetry | Partial — `cost_usd`/`tokens_used` columns shipped in `pkg/store/sqlite.go`, no dedicated branch |
| 9b | Python agent SDK | Shipped (`sdk/python/perchguard/`, PR #1) |
| 10a/10b/10c | K8s-native isolation operator (CRD, controller, EKS+Cilium) | Shipped — live-validated on EKS 2026-09-02, PR #3 fixed 3 bugs |
| 10d | Shadow-to-enforce rollout | Partial — policy gap closed (`configs/policies.yaml` allowedDestinations) + Hubble enabled/live-verified on EKS; the actual burn-in-then-flip rollout procedure is not built |
| 10e | Kata sandbox layer | Code complete, deployed live on EKS 2026-09-11 (webhook, DaemonSet, RuntimeClass, node group, TLS all applied without error) — kata-sandbox node group never scaled above 0, so kata-deploy's install has never actually run on a real node |
| 10f | Cross-layer conformance | Code written (`scripts/cross-layer-conformance.sh`, containment test pods, `kata-containment-check.sh`), never executed — depends on 10d/10e's live state |
| 11 | Multi-agent delegation showcase | Code complete (`sdk/python/perchguard/loop.py` local_tools routing, fraud-escalation scenario) — unit-tested only, no live run against real Claude (no API key in dev env) |

See `artifacts/docs/PHASE5-ADVERSARIAL-VALIDATION.md`, `PHASE6-OPEN-SOURCE-POLISH.md`, and
`PHASE10-K8S-ISOLATION-OPERATOR.md` ("Why this is priority #1") for the current sequencing
rationale — this project is a platform-engineering/agentic-systems portfolio showcase,
not a competing governance SaaS product, which is why Phase 10 (verifiable infra work)
outranks Phase 11 (app-layer framework integration).

## Key env vars

| Var | Default | Purpose |
|-----|---------|---------|
| `PERCHGUARD_ADDR` | `:8080` | Listen address |
| `PERCHGUARD_API_KEY` | auto-generated `pgmk-...` | Management API bearer key |
| `PERCHGUARD_LLM_API_KEY` | — | Anthropic key; enables semantic firewall |
| `PERCHGUARD_TLS_CERT` / `_KEY` | — | Enables HTTPS |
| `PERCHGUARD_POLICY` | `./configs/policies.yaml` | Policy file path |
| `PERCHGUARD_CONTEXT_ROOT` | — | Local context root for governance snapshots |

## Testing

```bash
go test ./...           # full suite
go test -race ./...     # race detector — run before every delivery
go build ./...          # build check
```

The declared-intent lab (register agent → fire calls → delete session → read governance.json)
is the integration smoke test. See `snapshots/governance.json` for expected output shape.
