# Architecture and Threat Model

This document describes how PerchGuard is built and what it defends against. It assumes
familiarity with the [Getting Started](getting-started.md) guide.

---

## 1. What PerchGuard is

PerchGuard is an admission controller for AI agent tool calls. It sits between an agent
(or the framework driving it — direct HTTP, an MCP proxy, a Copilot-style hook) and the
tools that agent is allowed to invoke, and applies the same pattern Kubernetes uses for
API objects: every proposed action is submitted as a review request, evaluated against
policy, and returned with a verdict before it is allowed to execute.

| Kubernetes admission control | PerchGuard |
|---|---|
| `AdmissionReview` | `ToolCallAdmissionRequest` |
| `AdmissionResponse` | `ToolCallAdmissionResponse` |
| Pod spec | `ToolCall` (name, parameters, destination) |
| `kubectl apply` | `agent.execute_tool()` |
| ValidatingWebhook | `Validator` |
| MutatingWebhook | `Mutator` |

The mapping is deliberate, not cosmetic — admission control is a well-understood pattern
(intercept → evaluate → allow/deny/mutate) and PerchGuard applies it to a different
substrate: agent actions instead of cluster state.

---

## 2. Architecture

### 2.1 Request flow

```
POST /intercept
    └── Interceptor.ServeHTTP
            ├── Token verification    (X-PerchGuard-Agent-Token, if registered)
            ├── Quota checks          (pkg/admission/quota/)
            ├── Validators            (pkg/admission/validator/, run in order, first DENY wins)
            ├── Mutators              (pkg/admission/mutator/)
            └── audit()               → TeeAuditLogger → stdout + AuditRingBuffer

POST /validate/output
    └── Interceptor.InterceptOutput   (outbound-only validators, e.g. OutputValidator)
```

A second pipeline runs on the return path: once a tool executes, its output can be sent
to `/validate/output` before being fed back into the agent's context. This matters because
indirect prompt injection lives in tool output, not agent input — a malicious instruction
embedded in a scraped webpage or a file's contents never appears in `user_intent`.

### 2.2 Components

**Validators** (`pkg/admission/validator/`) — each implements `Validate(ctx, req) *PolicyViolation`.
They run in sequence; the first violation with decision-overriding severity short-circuits
the pipeline.

| Validator | Question it answers |
|---|---|
| `PromptInjectionValidator` | Does `user_intent` or conversation history contain an injection pattern? |
| `ToolAuthorizationValidator` | Is this tool in the agent role's allow list? (prefix-match aware — `bash:ls` cannot smuggle in `bash`) |
| `DataExfiltrationValidator` | Is the call's destination an unknown or blocklisted egress target? |
| `LineageValidator` | Does this call consume a tracked data ref and route it to an external destination? (catches "read file → summarize → exfiltrate" chains across calls) |
| `PIIBiometricValidator` | Does the call's parameters or output contain PII/biometric-shaped content, independent of field-name redaction rules? |
| `SemanticFirewallValidator` | Does the proposed action actually serve the agent's declared mission/scope? (LLM-backed, requires `PERCHGUARD_LLM_API_KEY`) |
| `OutputValidator` | Does the tool's *output* contain injected instructions, before it re-enters the agent's context? (outbound-only) |

**FleetManager** (`pkg/agent/`) is session-stateful, unlike the validators above which are
stateless per-call:
- `RiskAccumulator` (`risk.go`) — cumulative 0.0–1.0 score per session. Risk only increases
  within a session by design: a session that drifted once stays suspect even if it
  temporarily behaves. Crosses `RiskThresholdHumanReview` (0.7) → escalate; crosses
  `RiskThresholdTerminate` (0.9) → kill the session.
- `IntentModel` (`intent.go`) — word-frequency cosine similarity between the proposed call
  and the agent's declared mission baseline(s) (no external ML dependency). This is what
  Phase 8's drift-timeline report is built on: a durable, per-call drift score that
  survives session deletion, used as Art.26(3)-style evidence that behavior stayed within
  declared bounds over the life of a long-running agent.
- `delegation.go` — scope and budget comparison for sub-agent delegation (parent budget
  cannot be exceeded by children).

**Mutators** (`pkg/admission/mutator/`) — transform parameters rather than blocking outright
(e.g. `parameter_sanitizer.go` injects `--dry-run` on destructive shell commands, strips
path traversal).

**Quota** (`pkg/admission/quota/`) — `session_budget.go` tracks token/cost spend per session
and triggers `TERMINATE` on exhaustion; `pricing.go` maps model usage to cost.

**Human review escalation** — when a validator's violation maps to `DecisionHumanReview`,
the call blocks on `pkg/review.Store.Wait` (in-process) or, for external operators,
`pkg/humanreview.Dispatcher` POSTs to a configured webhook and either gets a synchronous
200 decision or polls a 202 status URL. An operator approves/denies via dashboard, Slack,
or any system that speaks the webhook contract. Timeout defaults to **deny**.

**Audit** — every decision (`ALLOW`/`DENY`/`MUTATE`/`HUMAN_REVIEW`/`TERMINATE`) is written
to `AuditEntry` regardless of outcome — there is no code path that produces a decision
without an audit record. Entries carry `policy_version` (sha256[:8] of the active
`policies.yaml`), cumulative risk, drift score, and lineage refs, so the audit trail alone
is enough to reconstruct why a call was decided the way it was, without needing to replay
the request.

### 2.3 Decision model

```go
DecisionAllow       // proceed
DecisionDeny        // blocked, error returned to agent
DecisionMutate      // proceed with modified parameters
DecisionHumanReview // pause, escalate to a human
DecisionTerminate   // kill the session (budget exceeded, runaway loop, risk >= 0.9)
```

### 2.4 Trust boundary / auth split

- `/intercept`, `/validate/output`, `/agents/register` — **unauthenticated by design**.
  These are the agent-facing surface; an agent has no prior credential before it registers,
  and the entire point of the pipeline is to evaluate calls from parties that haven't
  proven trustworthiness yet. Authenticity of the *agent identity* (not the HTTP caller)
  is established separately via the manifest token issued at registration.
- `/api/*` (management API: keys, policy reload, audit query, dashboard) — requires
  `Authorization: Bearer <PERCHGUARD_API_KEY>`, enforced by `requireAPIKey` middleware in
  `RegisterRoutes` (`pkg/api/server.go`).

This split is the single most important trust-boundary decision in the system: it means
PerchGuard's own admission pipeline must assume **every caller of `/intercept` is
adversarial** — it cannot lean on HTTP auth to filter out attackers the way the management
API does.

---

## 3. Threat model

### 3.1 What PerchGuard protects

The asset is **the tool-execution boundary**: the moment an agent's decision becomes a
real side effect (file write, shell command, HTTP call, database write). PerchGuard does
not protect the agent's reasoning, the LLM provider, or the host the agent runs on — those
are out of scope (§3.4).

### 3.2 Adversary model

Three distinct threat actors, not one:

1. **The agent itself, manipulated.** The agent is not malicious by design, but its
   inputs are attacker-influenced — a user can prompt-inject it directly, or a tool's
   output (a scraped page, a file, an API response) can carry an indirect injection that
   redirects the agent's next action. This is the primary adversary PerchGuard was built
   for: not a hostile operator, but a cooperative agent acting on poisoned context.
2. **A compromised or malicious tool/MCP server.** The tool the agent calls may itself
   be attacker-controlled, returning output crafted to manipulate the agent on the return
   path, or silently exfiltrating data it was given. `DataExfiltrationValidator`,
   `LineageValidator`, and the outbound `OutputValidator` exist for this actor specifically.
3. **A network attacker hitting PerchGuard's own HTTP surface.** Since `/intercept` is
   unauthenticated by design, anyone who can reach it can submit admission requests.
   PerchGuard's own resilience (input bounding, no information disclosure in errors,
   constant-time token comparison) is the control here — see `SECURITY_CHECKLIST.md`.

### 3.3 Attack categories and mitigations

| Category | Technique | Mitigation | Validated by |
|---|---|---|---|
| Injection | Direct injection in `user_intent` | Pattern match | `PromptInjectionValidator` |
| Injection | Indirect injection via tool output | Outbound scan before re-entering agent context | `OutputValidator` |
| Injection | Multi-turn injection buried in conversation history | Full conversation passed to validator, not just the latest turn | `PromptInjectionValidator` |
| Authorization | Tool not in role's allow list | Allow-list check | `ToolAuthorizationValidator` |
| Authorization | Wildcard bypass (`bash:ls` masquerading as scope for `bash`) | Prefix-aware matching, not substring | `ToolAuthorizationValidator` |
| Authorization | Sub-agent delegation exceeding parent's budget/scope | Scope + budget comparator | `pkg/agent/delegation.go` |
| Exfiltration | Call routes to a known-bad or unlisted destination | Allow/deny-list on `DestinationURL` | `DataExfiltrationValidator` |
| Exfiltration | Multi-step exfiltration (read → transform → send), no single call looks malicious | Lineage refs tracked across calls within a session | `LineageValidator` |
| Exfiltration | PII/biometric leakage not caught by field-name redaction | Content-shape detection independent of parameter naming | `PIIBiometricValidator` |
| Session manipulation | Budget exhaustion / runaway loop | Cumulative cost tracking, `TERMINATE` on exhaustion | `pkg/admission/quota/` |
| Session manipulation | Recon → exploit → exfiltrate attack chain across many calls, each individually benign | Cumulative risk accumulator, monotonic within session | `pkg/agent/risk.go` |
| Session manipulation | Gradual semantic drift off the declared mission | Cosine-similarity drift against mission baseline(s) | `pkg/agent/intent.go`, `SemanticFirewallValidator` |
| Session manipulation | Replay of an old/expired token | TTL check on manifest token verification | manifest store |

This table is also the structure of the red-team harness (`docs/red-team-guide.md`,
`cmd/redteam-agent/`) — 18 scripted scenarios spanning these four categories, run against
a live deployment in observe mode before any enforcement decision is trusted.

### 3.4 Known residual risk and explicit non-goals

- **The semantic firewall is probabilistic, not a hard gate.** Phase 5 found a real gap
  (F1): an action with a plausible-sounding justification scored above the alignment
  threshold and let SSN data through a `write_file` call. The fix (mission context in the
  firewall prompt, explicit out-of-scope checking) closed that specific case, but an
  LLM-backed validator is inherently bypassable by a sufficiently well-crafted
  justification. Treat it as a second layer, not the only layer — content-shape validators
  (`PIIBiometricValidator`, `DataExfiltrationValidator`) exist precisely because they don't
  depend on the firewall's judgment call.
- **PerchGuard does not protect against a compromised LLM provider or host.** If the
  model itself is backdoored, or the machine PerchGuard runs on is compromised, the
  admission pipeline's guarantees do not hold — it is a control on the action boundary,
  not a sandbox or a model-integrity check.
- **PerchGuard is itself a single point of trust.** If `/intercept` evaluation can be
  bypassed or PerchGuard's own state (token store, policy file, audit log) can be tampered
  with, every downstream guarantee collapses. This is why `SECURITY_CHECKLIST.md` treats
  PerchGuard's own code — constant-time comparisons, `crypto/rand`, audit-write coverage on
  every decision path, no silent drops — as non-negotiable rather than ordinary hygiene.
- **Human review has a fail-closed timeout, not a fail-open one** — if the reviewer
  doesn't respond within `TimeoutSeconds`, the call is denied. This is a deliberate
  posture: an unanswered escalation is treated as "no," not "proceed."
- **Out of scope:** model fine-tuning/alignment, network-layer egress enforcement
  (PerchGuard flags a bad destination; it does not firewall the host's network),
  and authenticity of the human approving a `HUMAN_REVIEW` request beyond whatever the
  webhook/dashboard's own auth provides.

---

← [Docs index](README.md)
