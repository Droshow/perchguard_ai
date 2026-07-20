# PerchGuard Phase 9b — Python Agent SDK

**Date**: 2026-07-09
**Branch**: `demo-admissions` (spec only — no code branch cut yet)
**Status**: Spec / pre-planning — not yet approved for implementation
**Depends on**: Phase 2/3 admission pipeline (`/intercept`, `/validate/output`), Phase 3 agent
fleet (`/agents/register`, `pkg/agent/fleet.go`)
**Relates to**: `PHASE9-CHATOPS-AND-INFRA-GOVERNANCE.md` (on `regentics-website`, unmerged) —
that document already claims the bare "Phase 9" label for a still-undecided chatops/infra-
governance fork and explicitly says it does not resolve the numbering clash with Phase 8's
"Phase 9+ = vertical agents" claim. This document sidesteps the fight by taking the `9b`
slot: a concrete, scoped deliverable that is useful regardless of how that fork resolves.

---

## 1. The Problem

`deployments/insurance-agent-python/agent.py` hand-rolls the full governance loop inline:
build the Anthropic client, call `pg.intercept()` before every tool execution, call
`pg.validate_output()` after, branch on `DENY`/`MUTATE`/`TERMINATE`, splice results back into
`messages`. `admission_client.py` is a 60-line hand-written HTTP client with no retry, no
typed exceptions beyond one blanket `TERMINATE`-on-any-4xx/5xx, and no session lifecycle
(no call to `POST /agents/register`, no cleanup call to `DELETE /api/sessions/{id}`).

Two labs already show what happens without an SDK boundary: `healthcare-agent-python/agent.py`
solves the *same* problem a different way — via PerchGuard's transparent MCP proxy
(`pkg/mcp/proxy.go`, `--mode=wrap`) instead of explicit `/intercept` calls — and the two
`agent.py` files have drifted apart on unrelated details (iteration ceiling, session ID
prefix, print formatting) despite implementing the same governance contract. Every future
lab (and every third-party agent PerchGuard wants to govern) either copies 90 lines of
boilerplate or reinvents it slightly differently. That's a support burden and, worse, a
correctness risk: a copy-pasted `admission_client.py` that silently drops the
`DENY`/`TERMINATE` branch is invisible until someone red-teams it.

The declared-intent registration gap compounds this. `pkg/agent/fleet.go:102`
(`SeedFromManifest`) exists specifically so a session's drift baseline is seeded *before* the
first tool call — phases, out-of-scope vocabulary, parent session for delegation — but no
Python lab calls `POST /agents/register` at all. `run_agent()` mints a bare `session_id` and
starts firing `/intercept` calls cold, so `FleetManager.getOrCreate` (`fleet.go`) creates the
session with an empty intent baseline instead of the one `SeedFromManifest` would have built.
The capability is real; nothing in the Python surface makes it easy enough to use.

## 2. What Already Exists

- **`admission_client.py`** (`deployments/insurance-agent-python/admission_client.py`) — a
  `Decision` dataclass and a `PerchGuard` class with `intercept()` and `validate_output()`.
  This is the right shape for the SDK's low-level transport; it's just unpackaged, untested,
  and duplicated.
- **`mcp_client.py`** — bare JSON-RPC `tools/call` client, orthogonal to governance.
- **Two competing integration patterns**, both legitimate, that the SDK needs to support
  without picking a winner:
  1. *Explicit* (insurance lab) — the agent code calls `/intercept` and `/validate/output`
     itself, tool call by tool call.
  2. *Transparent* (healthcare lab) — the agent talks to what looks like a normal MCP
     server; PerchGuard's proxy (`pkg/mcp/proxy.go`, `cmd/wrap.go`) intercepts underneath,
     and the agent code never mentions PerchGuard at all.
- **`POST /agents/register`** (`pkg/api/server.go:92` → `FleetManager.SeedFromManifest`) —
  accepts `intentText`, `phases`, `outOfScope`, `manifestID`, `manifestVersion`,
  `parentSessionID`. Fully built server-side, zero Python client coverage.
- **`DELETE /api/sessions/{id}`** — evicts a `SessionAgent` (`fleet.go` `Evict`). Also
  uncalled from any Python lab, so lab runs leak sessions until the in-memory map is
  restarted.

## 3. Design Goals

- **One import replaces `admission_client.py` + the inline loop.** A new agent should be
  governance-correct by construction, not by careful copy-paste.
- **Cover both integration patterns**, not just the explicit one this ticket started from —
  the SDK should be the thing `healthcare-agent-python` *could* have used too, even though
  it took the proxy route instead.
- **Session lifecycle is a first-class SDK concern**: register (with manifest) → run →
  evict, not left to the caller to remember.
- **Typed decisions, typed failures.** `Decision.action` becomes an enum
  (`ALLOW`/`DENY`/`MUTATE`/`TERMINATE`/`HUMAN_REVIEW`), not a bare string compared with
  `==` scattered through caller code — this is exactly the kind of loose contract that
  hides a dropped branch.
- **Framework-shaped, not framework-owning.** The high-level loop helper is optional sugar
  over the low-level client — someone building a custom loop (LangChain, a non-Anthropic
  model, a different tool-call shape) still gets typed `/intercept` / `/validate/output` /
  `/agents/register` primitives without being forced into PerchGuard's loop shape.

## 4. Non-Goals

- Not a rewrite of the admission pipeline or any Go code — this is a client library only.
- Not a replacement for the MCP-proxy (`--mode=wrap`) integration path — that remains the
  zero-touch option for agents that don't want any PerchGuard-aware code at all.
- Not multi-language yet. Scope is Python, matching the two existing Python labs. A Go or
  TypeScript SDK is a separate, later decision — do not design this one in a way that
  requires speculative genericity for languages that don't have a concrete caller yet.
- Not a new governance feature. Every capability the SDK exposes (`intercept`,
  `validate_output`, `register`, `evict`) already exists server-side; the SDK is packaging
  and ergonomics, not new pipeline behavior.

## 5. Proposed Package Surface

Package name: `perchguard` (importable as `from perchguard import ...`). Ships two layers.

### 5.1 Low-level client — typed transport, one per concept already on the server

```python
from perchguard import PerchGuardClient, Decision, Action

pg = PerchGuardClient(base_url="http://localhost:8080")

session = pg.register(
    agent_id="insurance-agent",
    agent_role="developer_agent",
    intent="Process claim #4471 for policy holder...",
    phases=["verify policy", "assess claim", "issue determination"],
    out_of_scope=["payment execution", "policy modification"],
)  # -> Session(id=..., manifest_id=..., manifest_version=...)

decision: Decision = pg.intercept(
    session=session,
    uid=tool_use_block.id,
    tool_name=tool_use_block.name,
    parameters=tool_use_block.input,
)
if decision.action in (Action.DENY, Action.TERMINATE):
    ...

out: Decision = pg.validate_output(session=session, output=raw_tool_output)

pg.evict(session)  # or `with pg.register(...) as session:` for auto-evict
```

This is a near-1:1 typed port of the existing `admission_client.py`, plus the two missing
calls (`register`, `evict`) and real error handling (a distinct `PerchGuardUnavailableError`
for transport failures vs. a legitimate `TERMINATE` decision — today both collapse into the
same `TERMINATE` action, which makes "PerchGuard is down" indistinguishable from "PerchGuard
terminated this session for cause" in caller code).

### 5.2 High-level loop helper — optional, for the explicit-governance pattern

```python
from perchguard import GovernedAgentLoop
import anthropic

loop = GovernedAgentLoop(
    anthropic_client=anthropic.Anthropic(),
    perchguard_url="http://localhost:8080",
    mcp_url="http://localhost:8090",
    model="claude-haiku-4-5-20251001",
)

result = loop.run(
    task="Process claim #4471...",
    agent_id="insurance-agent",
    agent_role="developer_agent",
    tools=TOOL_DEFINITIONS,
)
```

`GovernedAgentLoop.run()` is the entire body of today's `run_agent()` — the `while
iteration < max_iterations` loop, both governance calls, the `messages` splicing — collapsed
into the SDK. `deployments/insurance-agent-python/agent.py` becomes a task-list driver that
imports this and stops owning any governance logic at all.

The transparent/proxy pattern (`healthcare-agent-python`) does not need `GovernedAgentLoop`
— it needs nothing, by design. Worth stating explicitly in the SDK README so the two
patterns aren't presented as one superseding the other.

## 6. Packaging & Distribution

Per [[project_perchguard_repo_split]], public-facing developer tooling belongs in the OSS
repo (`Droshow/perchguard_ai`), not the private `perchguard/PerchGuard` repo — this SDK is
exactly that: a thing third-party agent authors install, not internal governance logic.
Proposed: `perchguard_ai/sdk/python/`, published to PyPI as `perchguard` (name availability
unchecked — confirm before committing to it). Versioned independently of the server
(`perchguard-sdk==0.1.0` against a `PERCHGUARD_URL` server of any compatible minor version),
since the HTTP contract (`/intercept`, `/validate/output`, `/agents/register`) is the
compatibility boundary, not a shared release train.

## 7. Migration Plan (proof, not scope creep)

1. Build the SDK against the existing `/intercept` / `/validate/output` / `/agents/register`
   contracts — no server changes required.
2. Port `insurance-agent-python` to `GovernedAgentLoop`, deleting `admission_client.py`. This
   is the acceptance test: if the ported lab produces the same red-team pass rate as
   `PHASE7-INSURANCE-AGENT-TEST-RESULTS.md`, the SDK is behaviorally equivalent.
3. Leave `healthcare-agent-python` on the MCP-proxy pattern untouched — it's the deliberate
   control case proving the SDK isn't mandatory for governance to work.
4. Only after (2) passes: consider whether `redteam-mcp-agent` or future verticals adopt it.

## 8. Open Questions

- **PyPI package name** — `perchguard` may already be taken; needs a check before any code
  is written.
- **Async support** — today's labs are synchronous (`requests`). Worth a sync-only v0.1 and
  an `AsyncPerchGuardClient` later once there's a real async caller, or build both now? Leans
  sync-only first per §4's anti-speculative-genericity goal, but flagging since agent
  frameworks increasingly assume `asyncio`.
- **Auth** — `/intercept` etc. are unauthenticated by design (per `CLAUDE.md`, agent
  endpoints are open; only `/api/*` requires the bearer key). Does a public SDK talking to a
  self-hosted PerchGuard instance need an opt-in agent-token field for deployments that add
  their own gateway auth in front, or is that out of scope for v0.1?
- **Where does `GovernedAgentLoop`'s tool-execution step live?** The sketch in §5.2 assumes
  it still calls out to a caller-supplied MCP client (matching `mcp_client.py`) rather than
  owning tool execution itself — confirm that's the intended boundary before implementing.
