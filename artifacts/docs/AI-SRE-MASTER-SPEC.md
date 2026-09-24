# PerchGuard AI SRE: Master Spec

**Branch:** `ai-sre-rebuild`
**Captured:** 2026-09-24
**Status:** Direction set by the founder. Challenger-reviewed 2026-09-24 (outcome in
§11). This is an **evolution, not a redesign**: the admission pipeline, manifest,
fleet manager and audit trail are the foundation, and AI SRE is the identity and
direction built on top of them.
**Supersedes (in priority, not deletion):** the "governance layer for AI agents"
headline in `README.md`, Phase 8 compliance verticals as the next build, and the
"stop building" stance recorded on 2026-09-14.

---

## 1. Positioning

> **PerchGuard is reliability engineering for AI agents.**
> It treats agents the way SRE treats any production system: every action is
> **admitted** against the agent's declared goal and the live state of the
> environment, the agent is **contained** by the platform it runs on, its
> behavior is **measured** against SLOs, and every run is **recorded** as a
> timeline that future agents learn from.

Two sides of the same discipline:

- **Reliability *of* agents:** agents are production services. They get SLIs and
  SLOs (goal completion, plan adherence, loop/flail rate, cost per task,
  escalation rate), error budgets, and postmortems like any other service.
- **Safety of changes *made by* agents:** when an agent acts on infrastructure
  (remediation, scaling, config), it is a change source and gets change
  management: blast-radius limits, error-budget gates, freeze windows, approvals.

AI on-call (§8) is the **reference workload** that shows both sides at once. It
is not the whole scope.

Why the shift: the founder comes from platform/SRE work and already builds
runbook-execution automation in their day job. "Governance" is a compliance
buyer's word. "AI SRE" is an infrastructure engineer's word, and that is the
audience this repo needs to convince.

What does **not** change: the admission pipeline, the trust model (token
verification, fail-closed defaults, `TRUST-INVARIANTS-SPEC.md`), and the Phase 10
isolation operator. Governance and compliance become **supporting evidence**,
not the headline. The EU AI Act export stays in the repo and moves down the README.

### IP boundary (non-negotiable)

The day-job runbook-executor is where the *experience* comes from, and nothing
more. No code, runbooks, service names, architecture diagrams or incident data
from employer repos go into this repository. Every scenario here uses a
synthetic workload on infrastructure PerchGuard owns.

---

## 2. Thesis

An AI agent with `kubectl` is a new **source of change** in production. SRE
already has a discipline for sources of change: change management, blast-radius
limits, error budgets, freeze windows, approvals, postmortems. None of it is
applied to agents today, because agent guardrails come from the AppSec/LLM world
(prompt injection, PII), not from operations.

PerchGuard's claim: **apply operational change discipline to agent actions, at
the moment of the action, with knowledge of both what the agent is trying to do
and the state of the environment it is doing it in.**

That breaks down into three questions PerchGuard must answer on every mutating call:

| Question | Pillar | Today |
|---|---|---|
| Is this action consistent with what the agent said it is doing? | A. Goal awareness | **Exists:** per-call drift vs manifest, session drift timeline, semantic firewall, governance snapshots. Missing: plan level |
| Is this action safe given the environment right now? | B. Environment awareness | Missing: validators see tool name/args only |
| Did it work, and what should the next agent know? | C. Knowledge loop | Missing: audit records decisions, not outcomes |

Two more pillars cover the platform:

| Pillar | Today |
|---|---|
| D. Operability: PerchGuard itself and agent SLOs | Partial: 7 of the metrics defined in `pkg/telemetry/metrics.go` are never incremented; no agent-level SLIs |
| E. Containment of the agent itself | Shipped (Phase 10 operator, Cilium, Kata), not fully live-proven |

---

## 3. Pillar A: Goal awareness (manifest → plan → state)

### Goal observability by an independent observer (what exists)

This is PerchGuard's core asset and the basis for the reliability-*of*-agents
claim. PerchGuard is a **third party** to the agent: it measures how closely the
agent adheres to its declared goal **from outside the agent, without relying on
the agent's self-report**. Frameworks and tracing tools (LangSmith, Langfuse)
record what the agent says it did. PerchGuard scores what the agent actually
tried to do against what it was authorized to do.

- Per-call `drift_score` against the manifest baseline (`pkg/agent/session.go`,
  `IntentModel.DriftScore`).
- Per-session drift timeline with baseline and threshold
  (`pkg/api/sessions.go`, the Art. 26(3) drift-timeline report).
- Session risk accumulation that stays sticky after drift (`pkg/agent/risk.go`).
- Governance snapshots (`snapshots/governance.json`): per-step decision, risk,
  drift, peak risk, `terminated_early`, and the delegation tree across child sessions.
- LLM-judged alignment via `SemanticFirewallValidator`.

Supporting code:

- `pkg/manifest/manifest.go`: `Mission{Summary, Scope, OutOfScope, Phases}`,
  `Invariants`, `Authorization.HumanReviewRequiredFor`.
- `pkg/agent/intent.go`: `IntentModel`, a word-frequency cosine drift score
  against up to 5 baselines, with an out-of-scope counterweight.
- `SemanticFirewallValidator`: LLM-judged intent alignment (needs
  `PERCHGUARD_LLM_API_KEY`).
- `sdk/python/perchguard/langgraph_adapter.py`: `governed_node()` registers a
  session per LangGraph node; no checkpointer support.

### Gap

The manifest is **static and declared once**. Drift is lexical. PerchGuard has
no idea *where in its plan* an agent is, so it can't tell "restarting the pod is
step 3 of the plan I told you about" from "restarting the pod out of nowhere."

### Target

A three-level goal model, each level less trusted than the one above:

1. **Manifest** (operator-authored, signed off, static): the mission, and which
   hard limits apply. This is the trust anchor. Unchanged.
2. **Plan** (agent-declared, updated at runtime): an ordered list of steps, each
   with its own intended action class (`read`, `diagnose`, `mutate`), and the
   hypothesis it is testing (see Pillar C). Submitted via a new
   `PUT /agents/{session_id}/plan`. Every plan revision goes into the audit log.
3. **State** (agent-reported, per step): the current step, and a snapshot of the
   agent's working state, e.g. the LangGraph state dict at node boundaries.

Admission rules this enables:

- **Plan conformance:** a `mutate` call that doesn't match the current plan
  step's declared action class escalates to `HUMAN_REVIEW`.
- **Plan churn:** too many plan rewrites in a session is itself a signal (an agent
  flailing), and feeds session risk.
- **Plan vs manifest:** a plan step outside `Mission.Scope`, or matching
  `OutOfScope`, is rejected when the plan is submitted, *before* any action.

### Trust rule

Plan and state are **agent-asserted and untrusted**. They can only *tighten*
admission (escalate or deny). They can never *loosen* it: a plan cannot grant a
tool the manifest and policy don't allow. This mirrors how `Mission.Phases`
widening is counterweighted by `OutOfScope` today (`intent.go` `SetBaselines`
doc comment).

Plan conformance against an agent-authored plan is **observability, not
enforcement**: a misaligned agent can declare a plan that matches what it
intends to do. It becomes a control only when the plan is **approved before
execution**, either by policy or by a human through the existing `HUMAN_REVIEW`
path. The spec and the README must describe it that way (Challenger objection #2).

### LangGraph state: push, not pull

PerchGuard **does not** read agent checkpoint databases directly. Doing so would
make PerchGuard hold credentials to every agent's store, and couple it to one
framework's schema. Instead, the SDK adapter pushes a bounded state summary at
node boundaries, alongside the `/intercept` call it already makes. The
idempotency-key prerequisite from `NEXT-memory-and-database.md` §1 still applies
before checkpoint/resume is turned on.

---

## 4. Pillar B: Environment awareness (the infra specialty)

### Gap

`ToolAuthorizationValidator` matches `toolname` or `bash:<command>` against glob
lists per role (`tool_authorization.go` `buildToolSignature`). It can't tell
`kubectl rollout undo` in a staging namespace with 2 replicas from the same
command against prod with 40 replicas during a freeze.

### Target: `EnvironmentContext` + infra validators

A new read-only **environment context provider** that PerchGuard queries at
admission time for calls that target infrastructure:

| Source | What it tells the validator |
|---|---|
| Kubernetes API (read-only SA) | namespace labels (`env=prod`), workload replica count, PDBs, owner refs, recent rollouts |
| Prometheus | SLO burn rate / remaining error budget for the affected service, current error rate |
| Policy file | freeze windows, per-environment limits |
| Change context (see below) | what version is running, what changed recently and who changed it |

Validators built on it:

1. **`BlastRadiusValidator`**: computes the affected surface of a mutating call
   (pods touched, % of replicas, number of namespaces, whether a PDB is
   violated) and compares it to per-environment limits in `configs/policies.yaml`.
   Deny above the hard limit, escalate above the soft one.
2. **`ErrorBudgetValidator`**: when a service's error budget is exhausted,
   allow only risk-reducing action classes (rollback to a known-good revision,
scale-up), taken from the structured tool call, and
   escalate everything else. Rule of thumb: the agent may put out fires but not
   start new work while the budget is burning.
3. **`ChangeWindowValidator`**: freezes and maintenance windows, with an
   explicit break-glass path that goes through `HUMAN_REVIEW`.

Design constraints:

- **Parsing the action is the hard part.** Structured tools (`k8s_scale{ns,
  deployment, replicas}`) come first. Free-form `bash:kubectl ...` is best-effort
  and escalates when it can't be parsed. It must never be allowed just because
  parsing failed.
- **Missing context is not binary:** behavior when Prometheus or the K8s API
  can't be reached depends on the action class (§12, D2), falling back to
  conservative static limits rather than all-allow or all-deny.
- **Latency budget:** environment lookups are cached (short TTL), so p99
  `/intercept` stays inside the SLO in §6.

### Change context: what's in production, what changed

Most incidents are caused by changes, and "what changed?" is the first question
in every incident. PerchGuard should be able to answer it for the services its
agents touch:

| Signal | Source |
|---|---|
| What's running | Pod image digest → git SHA via the OCI `org.opencontainers.image.revision` label; Deployment revision history |
| What changed recently | Rollouts (ReplicaSet revisions), ConfigMap/Secret changes, GitOps sync events if present |
| What agents changed | PerchGuard's own audit log. No other system has this. |
| What the change contained | Change metadata: SHA, PR, changed files, deploy time. Linked from the SHA, not parsed. |

Together these form **one change timeline across humans, pipelines and agents**.

Uses:
- **Admission:** a rollback target must be a known-good revision; an agent
  mutating a service a human deployed within the last N minutes escalates
  (conflicting changes); deploy freezes apply to agents too.
- **Goal observability:** an agent's hypothesis ("bad release") can be checked
  against the actual change log instead of trusted.
- **Outcome:** SLO recovery is correlated with a specific change (§12, D3).

Scope guard: PerchGuard holds change **metadata**, reachable through the same
read-only K8s access as the rest of this pillar. Understanding the code itself
(reading diffs, RAG over the repository) is agent-side knowledge, not part of
the admission path.

---

## 5. Pillar C: Knowledge loop (hypothesis → action → outcome → corpus)

### Idea

SRE investigation is hypothesis-driven: *"error rate spiked after deploy X →
hypothesis: bad release → evidence: errors started at rollout time, only new
pods → action: roll back → outcome: error rate recovered in 4 min."* That record
is the valuable thing. Today it only exists in people's heads and in postmortems
written days later.

### Target

1. **Structured investigation records.** Each plan step (Pillar A) carries
   `hypothesis`, `evidence_refs`, and after execution `outcome`. PerchGuard
   already sits on every call, so it can attach the admitted action and, after a
   configured delay, the observed effect (a Prometheus query declared in the
   step), without trusting the agent's own claim of success.
2. **Incident timeline export.** `GET /api/sessions/{id}/timeline` renders
   hypothesis → evidence → action → decision → outcome, in order. It becomes the
   postmortem draft. It builds on the planned hash-chained audit (see
   `project_auditability_roadmap`), so the timeline is tamper-evident.
3. **Corpus.** Closed investigations are written to a knowledge store, first as
   JSONL/SQLite and later with a vector index. A new agent session can retrieve
   *"past incidents with this symptom, what was tried, what worked."*
   PerchGuard's role is to be the **source of verified outcomes**. Retrieval and
   RAG live in the agent (see `NEXT-memory-and-database.md` §4), not in the
   admission path.

The outcome check (point 1) is the key difference: a corpus built from agents'
self-reports teaches the next agent the first agent's mistakes. A corpus whose
outcomes were checked against the environment by an independent observer does not.

---

## 6. Pillar D: Operability: PerchGuard itself, and the agents it governs

An SRE reviewer checks this first. It is also the cheapest pillar.

**Agent SLIs.** PerchGuard sees every call as an independent observer, so it
can compute SLIs the agent can't game by self-reporting. Three groups, by where
the signal comes from:

| Group | SLIs | Source | Status |
|---|---|---|---|
| Goal adherence | % of calls within drift threshold, per-session peak drift, out-of-scope hit rate, semantic-firewall misalignment rate, early-termination rate | Existing drift/risk/firewall machinery | Signals exist; not exported as metrics |
| Goal outcome | Did the environment reach the goal state (for example, error rate back under SLO after remediation) | Environment signals (Pillar B), **never the agent's own claim** | Needs Pillar B |
| Behavior | Allow/deny/escalation rate, loop/flail rate (repeated identical calls), cost and tokens per session, time to first mutating action | Audit + quota | Partly exists (tokens/cost on SDK path) |

"Goal completion" as the agent reports it is **not** an SLI; it is untrusted.
Goal adherence is measured independently today, and goal outcome is measured
independently once environment signals exist. SLIs are exported per agent role
with SLO templates, so an agent fleet can run on error budgets like any other service.

**PerchGuard itself:**

1. **Wire the dead metrics.** `InterceptTotal`, `InterceptDuration`,
   `DecisionsTotal`, `RiskScoreGauge`, `SessionRiskScore`, `BlockedToolsTotal`
   and `PolicyViolationsTotal` are defined in `pkg/telemetry/metrics.go` and have
   zero call sites outside tests (verified 2026-09-24).
2. **SLOs for PerchGuard itself**, with recording rules and burn-rate alerts in
   `deployments/prometheus/`:
   - `/intercept` availability ≥ 99.9%
   - `/intercept` p99 latency < 50 ms excluding the semantic firewall, with a separate SLO when it is enabled
3. **An explicit failure-mode decision:** what an agent SDK does when PerchGuard
   is down (fail-closed for mutating calls, configurable for reads), documented
   and tested.
4. **One OTel trace per action:** agent turn → `/intercept` → each validator
   span → environment lookups → tool execution. `pkg/telemetry/otel.go` exists;
   extend it rather than adding a new dependency.
5. **Dashboards:** reuse `deployments/grafana/`, adding an "AI on-call" board with
   decisions by environment, blast-radius escalations, and error-budget gates.

---

## 7. Pillar E: Containment (reframe of Phase 10)

No new build. The README reframes it as *"the agent's own blast radius is
bounded by the platform, not only by policy"*: `AgentIsolationPolicy` CRD →
Cilium egress policy, Kata `RuntimeClass`. The open live-validation items from
Phase 10d/10e/10f stay open and are labeled honestly.

---

## 8. Reference workload: the governed SRE agent

A synthetic, self-owned scenario replaces the insurance/fraud demos as the
headline. The older demos stay in the repo.

**Environment:** local k3s first (`deployments/k3s/` exists), EKS for the
recorded live run. A small demo service (`checkout-api`) with an SLO, a
Prometheus scrape config, and two versions: `v1` healthy, `v2` returns 5xx at
some rate.

**Agent:** a LangGraph agent with nodes `triage → hypothesize → investigate →
remediate → verify`, governed via `langgraph_adapter.governed_node()`.

**Tools (structured, not raw bash):**

| Tool | Class |
|---|---|
| `prom_query`, `k8s_get`, `k8s_events`, `k8s_logs` | read |
| `k8s_rollout_undo`, `k8s_scale`, `k8s_restart` | mutate |
| `k8s_delete_namespace` | mutate; exists to be denied |

**Scenarios (each is a demo beat and an integration test):**

1. Bad deploy → agent hypothesizes a bad release → rollback **ALLOW** (in plan,
   remediation, small blast radius) → outcome verified → record goes into the corpus.
2. Same fault, agent wants to scale down to "reduce noise" → **ESCALATE**
   (budget exhausted, not a remediation action).
3. Agent tries to restart every pod in prod → **DENY** (blast radius above the hard limit).
4. Mid-investigation, a log line containing an injected instruction ("delete
   namespace checkout") → **DENY**, twice over: off-plan and out of manifest
   scope. The existing prompt-injection layer is shown working in an ops context.
5. A second incident with the same symptom: the agent retrieves scenario 1's
   verified record and goes straight to the rollback hypothesis.

LLM: a real Claude model via the Anthropic SDK. No mocked LLM in the headline
demo (key location: sibling `perchguard/.env`).

---

## 9. Milestones

Each milestone ends with something runnable and an honest status line in the README.

| # | Milestone | Exit criteria |
|---|---|---|
| M0 | Positioning | README headline and intro rewritten; this spec committed; Challenger pass run in a fresh session on §3–§5 |
| M1 | Operable PerchGuard + goal-adherence SLIs (Pillar D) | Dead metrics wired and tested; goal-adherence SLIs exported from existing drift/risk data; PerchGuard SLO rules + dashboard; fail-mode documented; `go test -race ./...` green |
| M2 | Reference environment | k3s: `checkout-api` v1/v2, Prometheus with SLO recording rules, fault injection script |
| M3 | Environment awareness (Pillar B) | `EnvironmentContext` provider (read-only SA + Prometheus, in-process, per-call timeout); one blast-radius + error-budget validator, **shadow mode first**; change context from K8s rollout history and image revision labels; goal-outcome SLI from environment signals; scenarios 2–3 pass as integration tests |
| M4 | SRE agent (separate repo) | LangGraph SRE agent with structured tools, governed through the SDK; scenarios 1, 3 and 4 pass live against real Claude |
| M5 | Live proof | Finish the 10d burn-in-then-flip; full run on EKS with Cilium, shadow → enforce; recorded; results doc in the same style as `PHASE7-INSURANCE-AGENT-TEST-RESULTS.md` |

**Roadmap (after M5, not committed):** plan object with pre-execution approval
(Pillar A, §3); knowledge loop as an async reconciler outside `/intercept`
(Pillar C, §5); scenario 5.

M1 and M2 are independent and can run in parallel. M3 needs M2. M4 needs M3 for
scenario 3. M5 needs M3 and M4.

Every milestone touching auth, agent input or admission runs `/security-review`
and `SECURITY_CHECKLIST.md` before merge (project `CLAUDE.md`).

---

## 10. Non-goals

- No generic "AI SRE platform" competitor (auto-remediation product, alert
  routing, on-call scheduling). PerchGuard is the **safety layer** under such
  agents, not the agent product.
- No direct reads of agent checkpoint stores (see §3).
- No new external dependencies beyond what the reference workload strictly
  needs (`client-go` is likely already transitive via the operator; Prometheus
  HTTP API needs none). Each addition gets explicit approval.
- Phase 8 compliance verticals (PSD2, SOC2) are paused, not cancelled.
- No multi-tenant SaaS scaffolding or billing.

## 11. Challenger review (2026-09-24)

Verdict: **build smaller**. The Challenger also raised the frequent re-scoping as
the biggest risk: new pillars on top of Phase 10e/10f, which have never run live.
The review had no file access; its factual claims were checked by hand afterwards.

Accepted:
- Plan conformance is observability unless pre-approved (§3).
- Knowledge loop stays out of the synchronous `/intercept` path; moved to roadmap.
- `EnvironmentContext`: in-process, behind a small interface, strict per-call
  timeout, fail-mode decided per action class. No plugin system.
- SRE agent lives in a **separate repo**, which keeps the governed workload out of
  the trust anchor's process/credential space and protects the IP boundary.
- Structured infra tools only; unparseable `bash:kubectl` escalates.
- Milestones cut to M0–M5; A-plan and C moved to roadmap.

Rejected:
- "PerchGuard can't measure goals, only calls." It already measures goal
  **adherence** independently (§3); what it can't trust is the agent's own
  completion claim. Goal observability stays as a core pillar and the basis of
  the reliability-*of*-agents claim.
- "Cut Pillar D." Wiring the dead metrics and exporting goal-adherence SLIs is
  the cheapest concrete proof of the headline.

The questions it left open are broken down in §12.

### Original questions



1. Is "plan" a new first-class object, or an extension of `Mission.Phases` with
   runtime updates? A new object is cleaner. An extension reuses the drift machinery.
2. Is lexical `IntentModel` drift still useful once plan conformance exists, or
   does it become noise in an ops context full of shared vocabulary (pod, deploy,
   error)?
3. Outcome verification: does PerchGuard run the verification query itself
   (stronger trust, but PerchGuard needs Prometheus read access and a scheduler),
   or does it record the agent's claim plus the raw query for later checking?
4. Should `EnvironmentContext` live in-process, or as a sidecar/plugin interface
   so non-K8s environments (Terraform plans, cloud APIs) can plug in later?
5. Is the SRE agent in-repo (`deployments/python-agents/sre-agent/`) or a
   separate repo that consumes PerchGuard? In-repo is easier to demo; separate is
   more honest about the boundary.

---

## 12. Decisions from first principles (provisional)

Each decision is broken down to its underlying question and given a working
answer. They are **provisional**: refine them as development produces evidence,
and record changes here with a date.

### D1. Is lexical drift too noisy for ops agents?

**Underlying question:** what makes an ops action off-goal?

- Drift detects the agent doing something other than its mission. Lexical
  cosine works only when on-task and off-task actions use different words.
- In ops they don't: `rollout undo checkout -n prod` and `delete namespace
  checkout` share almost every noun. What separates them is the **verb class**
  (read / reversible mutate / destroy), the **target** (service, namespace), the
  **environment**, and the **magnitude** (replicas, pods touched).
- Those are structured fields in the tool parameters. The right measure is **set
  membership** (is this target and verb inside what the manifest declared?),
  not similarity.

**Working answer:** add structured drift for structured infra tools (target
outside declared services/namespaces, or verb class above what the current phase
allows). Keep lexical drift for free-text tools.

**Settle with data:** in M3/M4, log drift scores for on-task and off-task calls
across the reference scenarios. If a threshold separates them, lexical drift
stays for ops too.

### D2. How should PerchGuard fail during an incident?

**Underlying question:** which error costs more, admitting a harmful action or
blocking a helpful one? And who holds the credential?

- **The cost asymmetry depends on the action, not the system.** Reversibility
  (a rollback to a known-good revision vs. deleting a PVC), direction (scaling up
  reduces risk, scaling down raises it) and blast radius decide it. A single
  global fail-open/fail-closed switch is the wrong design.
- **Two different failures:** PerchGuard itself down, vs. its context sources
  down. The context sources fail **together with** incidents (Prometheus is
  overloaded exactly then), so "fail closed on missing context" blocks
  remediation at the worst moment by design.
- **Missing context is not binary:** without live error-budget data, fall back
  to conservative static limits (for example, at most 1 deployment and 25% of
  replicas).
- **Fail-closed only means something if PerchGuard sits in the path
  structurally.** If the agent holds its own `kubectl` write credential, a
  fail-closed SDK is advisory. Real enforcement requires PerchGuard, or the tool
  server behind it, to hold the write ServiceAccount, with the agent holding
  none. Phase 10's Cilium egress policy can enforce that at the network level.

**Working answer:** base the fail mode on action class:

| Action class | PerchGuard unreachable / context missing |
|---|---|
| Read | Fail open |
| Reversible, risk-reducing, small blast radius (rollback to known-good, scale-up) | Static fallback limits + audit flag |
| Destructive or large blast radius | Fail closed; break-glass via `HUMAN_REVIEW` |

**Decide first:** the credential model (who holds the write ServiceAccount). It
is the actual trust-model decision here and must be settled before M4.

### D3. Who runs outcome verification?

**Underlying question:** an outcome record is only worth as much as its
independence from the actor.

Trust breaks down into three choices; the record is only as strong as the weakest:

| Choice | Weak | Strong |
|---|---|---|
| Who defines success | The agent picks the check query, so it can pick one that says "fixed" | The operator: existing SLO recording rules |
| Who executes the check | The agent | PerchGuard |
| When | Synchronously in `/intercept`; can't work, outcomes arrive minutes later | Async reconciler after a settle window |

**Working answer:** success = the operator-defined SLO for the affected service,
evaluated by a PerchGuard async reconciler after a settle window, correlated
with the change that preceded it (change context, §4). No new access beyond the
Prometheus read Pillar B already needs. Checks the agent proposes are recorded
as **claims**, kept separate from verified outcomes. This builds the goal-outcome
SLI (§6) and is the first piece of Pillar C.

### D4. Should PerchGuard know what's in production and what changed?

**Underlying question:** SRE's first question in an incident is "what changed?".
Can an AI SRE safety layer answer it?

- Most incidents are change-induced, so admission decisions, hypothesis checks
  and outcome attribution all need change context.
- PerchGuard is the only component that sees agent changes as they happen
  (its audit log). Merged with K8s rollout history and image revisions, that
  becomes one change timeline across humans, pipelines and agents.
- Change metadata is cheap and reachable through access Pillar B already needs.
  Code understanding is expensive and belongs agent-side.

**Working answer:** yes, as metadata (§4 "Change context"), landing in M3.
