# PerchGuard Phase 5 — Adversarial Validation

**Date**: 2026-05-06
**Branch**: `phase5-red-team-testing`
**Status**: Shipped — 2026-05-07

---

## 1. The Problem Phase 4 Left Unsolved

Phases 1–4 built a functionally complete admission pipeline and made it operable:

| Phase | What it added |
|-------|--------------|
| 1 | Quota, tool authorization, prompt injection, parameter mutation |
| 2 | Semantic firewall — intent-aligned per-call scoring |
| 3 | Agent fleet — session-stateful drift detection and attack chain recognition |
| 4 | Management API, audit ring buffer, LLM governance query, policy hot-reload |

Every scenario validated against this pipeline was hand-authored. A human decided which tool to call, what the payload was, and what order the calls arrived in. The pipeline has never faced a real agent generating novel inputs, drifting semantically, or probing for gaps in sequences a human wouldn't think to script.

That is the gap Phase 5 closes: making the system ready to face a real adversary — a red team running actual LLM-driven agents against a live PerchGuard deployment.

Five concrete deficiencies stand between the current state and a meaningful red team exercise.

---

## 2. The Five Gaps

### Gap 1 — Semantic firewall is effectively off

**Current state:** `SemanticFirewallValidator` runs only when `user_intent` is non-empty and `PERCHGUARD_LLM_API_KEY` is set. Every mock scenario either omits intent or runs without the API key. The most important governance layer — "does this call align with the declared mission?" — has never been tested under adversarial conditions.

**Why this matters:** A red team agent will declare a benign mission and then drift. Pattern-matching validators (tool authorization, data exfiltration) only catch known-bad tools and destinations. They cannot catch a semantically misaligned call that uses an allowed tool to do something the manifest never authorized. The semantic firewall is the only layer that catches mission-level deception.

**What Gap 1 requires:**
- `PERCHGUARD_LLM_API_KEY` set in the server environment
- A real agent (Claude via SDK) that sets `user_intent` from its system prompt on every `/intercept` call — not a hand-authored string
- A baseline false-positive rate established on normal sessions before adversarial runs begin

---

### Gap 2 — MCP proxy path is untested against a real upstream

**Current state:** Every test hits `/intercept` directly. The MCP proxy (`pkg/mcp/proxy.go`) intercepts `tools/call` and routes through the admission pipeline, but it has never been exercised with a live MCP server on the other side. The proxy's outbound scan (`scanOutput`) has the same blind spot.

**Why this matters:** A real Claude + MCP deployment routes through the proxy, not `/intercept`. Protocol-level differences — streaming, batch calls, tool schemas, content block types — may expose gaps invisible in direct HTTP tests. The proxy path also handles session lifecycle differently; it is unclear whether proxy sessions register in the fleet and whether governance snapshots emit correctly.

**What Gap 2 requires:**
- One real MCP server (insurance-mcp-server or healthcare-mcp-server in `deployments/`) running behind PerchGuard in proxy mode
- A real Claude agent session run through the proxy end-to-end
- Governance snapshot emission verified via the proxy path

---

### Gap 3 — Sessions have no expiry; evidence can be silently lost

**Current state:** A governance snapshot is only emitted when `DELETE /api/sessions/{id}` is called explicitly. If a session ends abnormally — network drop, agent crash, timeout, deliberate evasion — the session hangs open in the in-memory store indefinitely. No governance record is written. The attack is unrecorded.

**Why this matters:** A deliberate evasion technique is to stop sending calls before the risk accumulator crosses the TERMINATE threshold. The session never closes, the ring buffer fills with other sessions, and the audit trail for that session evaporates silently. The defender has no evidence the session existed.

**What Gap 3 requires:**
- Session idle timeout: if no tool call arrives within a configurable window (default 15 min), emit a governance record with `terminated_early: true` and evict the session
- Max session duration cap independent of call frequency
- Both configured in `configs/policies.yaml` under `sessionBudget`

---

### Gap 4 — Audit trail is ephemeral; forensics impossible after restart

**Current state:** `AuditRingBuffer` is in-memory with a fixed capacity of 1000 records. A server restart loses all ring buffer contents. The governance snapshot captures session-close state, but the `steps[]` trace is assembled from ring buffer contents at close time — if the buffer has rolled over or been lost before session close, steps are incomplete or missing.

**Why this matters:** Forensic reconstruction after a red team run requires a complete, durable audit trail. A multi-hour exercise with high call volume will roll the ring buffer multiple times. Any server restart (deliberate or otherwise) destroys in-flight session evidence.

**What Gap 4 requires:**
- An append-only JSONL file sink that writes each `AuditRecord` to disk at emit time, alongside stdout — low cost, survives restarts
- The JSONL file is the forensic record; the ring buffer remains the live query layer
- Postgres/Redis is explicitly out of scope for Phase 5 — that is a post-red-team hardening decision

---

### Gap 5 — No cross-session visibility during a live red team run

**Current state:** Governance data is visible post-hoc via `governance.json` and in real time only through stdout logs. During an active red team run with multiple concurrent sessions, there is no way to see the fleet-level picture: which session is escalating, what the aggregate risk landscape looks like, whether multiple sessions are coordinating.

**Why this matters:** Defenders need a live feed during the exercise. Without it, a red team can exhaust session budgets and terminate safely before the defender notices a pattern. Post-hoc analysis is too late if the goal is to observe and respond during the run.

**What Gap 5 requires:**
- `GET /api/fleet/summary` endpoint: aggregate risk distribution, sessions near threshold, TERMINATE events in the last N minutes — assembled from `sessions.List()` and `auditRing.Query()`, no new storage
- A minimal polling harness for the defender (a curl loop piped to jq is sufficient for a first red team day)

---

## 3. What Phase 5 Does Not Include

Phase 5 is scoped to red team readiness. The following are explicitly deferred:

- **Management API authentication** — completed before Phase 5 in the current branch (see `development/todo/2026-05-06-api-auth.md`)
- **Prometheus `/metrics` endpoint** — useful but not required for a controlled red team exercise
- **Postgres / Redis session store** — the JSONL append sink covers the forensics requirement for Phase 5
- **Dashboard UI** — the fleet summary API + a polling loop is sufficient for the exercise
- **Agent identity / token verification** — completed before Phase 5 in the current branch (see `development/todo/2026-05-06-api-auth.md`)

---

## 4. Capability Summary

| # | Capability | Gap closed | Key files (new or modified) |
|---|-----------|-----------|----------------------------|
| 1 | Semantic firewall live with real agent | Gap 1 | `configs/policies.yaml`, env wiring, agent SDK client |
| 2 | MCP proxy end-to-end against real upstream | Gap 2 | `pkg/mcp/proxy.go`, `deployments/insurance-mcp-server/` |
| 3 | Session idle expiry + max duration | Gap 3 | `pkg/store/memory.go`, `pkg/api/sessions.go`, `configs/policies.yaml` |
| 4 | Append-only JSONL audit sink | Gap 4 | `pkg/audit/jsonlsink.go` (new), `cmd/main.go` |
| 5 | `GET /api/fleet/summary` + defender polling harness | Gap 5 | `pkg/api/fleet.go` (new) |

---

## 5. Build Order

The gaps are not independent — the order matters.

**1. Gap 3 first (session expiry).** Operational hygiene. Without it, any abnormal session during red team setup silently loses its evidence. The simplest gap to close and the most dangerous to leave open.

**2. Gap 4 second (JSONL sink).** One new file, wired in `cmd/main.go`. Must be in place before the first adversarial session runs — forensics written after the fact is not forensics.

**3. Gap 1 third (semantic firewall live).** Highest yield for adversarial testing. Requires Gap 3 and Gap 4 to be complete so that semantic-firewall-driven sessions are fully recorded. Needs a real SDK-based agent client — this is where the red team payload is authored.

**4. Gap 2 fourth (MCP proxy end-to-end).** Validates the actual deployment topology. Blocked until Gap 1 is live, because MCP proxy sessions need intent context to exercise the full pipeline.

**5. Gap 5 last (fleet summary + defender harness).** Low complexity, can be built day-of the red team exercise. Depends on all prior gaps being complete so there is meaningful data to surface.

---

## 6. Red Team Scope (First Exercise)

Once Phase 5 is complete, the first red team exercise should target:

- **Mission drift:** register a benign manifest, progressively drift into out-of-scope behaviour across 20–30 calls; measure how many calls the semantic firewall catches vs. how many slip through
- **Tool-level evasion:** use only allowed tools but chain them in attack-chain sequences the `BehaviorAnalyzer` is not tuned to detect
- **Multi-session coordination:** two sessions operating independently, each below risk thresholds, whose combined effect constitutes an exfiltration
- **Proxy protocol edge cases:** malformed tool schemas, oversized content blocks, streaming calls — anything the `/intercept` tests did not cover
- **Evidence erasure:** attempt to exhaust the ring buffer before session close to corrupt the `steps[]` trace (Gap 4 should prevent this)

The output of the first exercise is a prioritized list of detection gaps and policy updates — not a pass/fail verdict.
