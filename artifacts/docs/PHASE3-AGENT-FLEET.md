# PerchGuard Phase 3 — Agent Fleet
## What It Is, How It Works, and Why It Exists

---

## 1. The Problem Phases 1 & 2 Left Unsolved

Phases 1 and 2 made PerchGuard a capable **per-call** admission controller.
Every tool invocation is inspected in isolation: is this specific `bash` call dangerous?
Does this `http_post` destination look suspicious?

That catches a lot. But sophisticated agent misbehaviour is a **sequence**, not a moment.

An attacker — or a compromised/runaway agent — does not open a session and immediately exfiltrate data.
It reads the filesystem (`web_search`, `list_directory`), establishes what is valuable, escalates access (`cloud:iam`, `sudo`), and *then* exfiltrates.
Each individual step may look benign. The arc is not.

Phase 3 adds a **stateful reasoning layer** that watches the full arc of a session.

---

## 2. Architecture Overview

```
Tool Call ──► [Quota]──►[Validation]──►[Mutation]──► Decision
                          │
                          ├── PromptInjectionValidator
                          ├── ToolAuthorizationValidator
                          ├── DataExfiltrationValidator
                          ├── SemanticFirewallValidator  (Phase 2)
                          └── FleetManager              (Phase 3) ◄── NOW
                                    │
                                    └── SessionAgent (one per session)
                                              │
                                              ├── IntentModel      (drift scoring)
                                              ├── BehaviorAnalyzer (attack chains)
                                              └── RiskAccumulator  (0.0–1.0 score)
```

> **Note — Phase 3 snapshot.** This diagram reflects the current implementation.
> When local context integration lands (Phase 4), `SessionAgent` will grow a context seeder
> that pre-loads the intent baseline from the context provider at session bootstrap
> instead of waiting for the first observed tool call. `IntentModel`, `BehaviorAnalyzer`,
> and `RiskAccumulator` are unaffected — the seeder is an additive slot on `SessionAgent`.
> The diagram will be updated once the ADR defines the data contract.

`FleetManager` implements the same `admission.Validator` interface as every other validator.
The admission engine (Phase 1) knows nothing about sessions — it just calls `Validate()`.
The fleet does all the stateful bookkeeping invisibly.

---

## 3. Components

### 3.1 `pkg/store` — Session Persistence

```
store.SessionStore  (interface)
store.MemoryStore   (in-memory dev implementation)
store.SessionState  { SessionID, RiskScore, Events[]ToolEvent, CreatedAt, UpdatedAt }
store.ToolEvent     { Tool, Stage, Timestamp }
```

`MemoryStore` is intentionally simple — a `sync.RWMutex` guarded `map[string]*SessionState`.
The `SessionStore` interface is the production extension point: swap in a Redis-backed implementation
without changing any agent code.

`ToolEvent.Stage` is a string that records which attack phase the tool belongs to
(populated by `BehaviorAnalyzer.ClassifyTool`).

---

### 3.2 `pkg/agent/intent.go` — Intent Drift Detection

```
IntentModel
    SetBaseline(text string)         — called on first tool call when UserIntent is present
    DriftScore(candidate string) float64  — returns 0.0 (on-task) to 1.0 (completely unrelated)
```

**How it works**: word-frequency cosine similarity.

1. The user's stated intent (e.g. *"refactor the authentication middleware"*) is tokenized into a word-frequency vector.
2. Every subsequent tool call's name + parameters are also vectorized.
3. Cosine similarity between the two vectors gives alignment: 1.0 = identical vocabulary, 0.0 = no overlap.
4. `DriftScore = 1.0 - similarity`.

**Why not neural embeddings?**
No external ML dependencies — the module only needs stdlib + yaml + OpenTelemetry.
Word-frequency cosine similarity is surprisingly effective at catching task hijacking:
a session declared as *"analyse logs"* that starts calling `cloud:iam` and `http_post`
will show high drift even without semantic understanding.

A threshold of **0.4** is the recommended starting point (configurable via `driftThreshold` in `policies.yaml`).
A drift above threshold contributes `drift × 0.3` to the session risk score.

---

### 3.3 `pkg/agent/behavior.go` — Attack Chain Detection

```
Stage: "recon" | "exploit" | "escalate" | "exfiltrate" | "unknown"

ClassifyTool(toolName string) Stage   — maps tool → stage
BehaviorAnalyzer.DetectChain(events []store.ToolEvent) bool
```

**How it works**: subsequence matching against known attack chains.

Each tool call is classified into an attack stage:

| Stage | Example Tools |
|-------|--------------|
| `recon` | `web_search`, `read_file`, `list_directory`, `glob`, `grep` |
| `exploit` | `bash`, `execute`, `run_command`, `shell` |
| `escalate` | `cloud:iam`, `database:admin`, `sudo`, `chmod` |
| `exfiltrate` | `http_post`, `upload`, `send_email`, `write_file`, `ftp` |

Known attack chains watched by the analyzer:

```
recon → exploit → exfiltrate   (classic steal-data)
recon → escalate → exfiltrate  (privilege escalation then exfil)
recon → exploit → escalate     (foothold then privilege gain)
```

Matching is **subsequence** (not contiguous): intervening `unknown`-stage calls
between recon and exploit do not break detection.
The window is configurable (`behaviorWindowSize`, default 10 events).

When a chain is detected: `+0.35` to session risk.

---

### 3.4 `pkg/agent/risk.go` — Risk Accumulator

```
RiskAccumulator
    Add(signal RiskSignal)    — adds contribution, clamps to 1.0
    Score() float64           — current cumulative risk
    Severity() string         — "low" | "medium" | "high" | "critical"
```

Risk **only increases** within a session. A session that has drifted once is permanently flagged —
partial normalization of behaviour does not reset the clock.

| Score | Decision |
|-------|----------|
| < 0.40 | No action (clean) |
| 0.40–0.69 | Medium — logged, no pipeline escalation |
| ≥ 0.70 | `HUMAN_REVIEW` — admission pipeline escalates |
| ≥ 0.90 | `TERMINATE` — session killed via `critical` violation |

---

### 3.5 `pkg/agent/session.go` — SessionAgent

One `SessionAgent` per session ID, owned by `FleetManager`.

**Evaluate() lifecycle per tool call:**

```
1. loadOrInit()          — load or create SessionState in store
2. SetBaseline()         — capture intent on first call (if UserIntent is set)
3. DriftScore()          — score this call against the intent baseline
4. if drift > threshold  — Add(drift × 0.3) to risk
5. ClassifyTool()        — assign attack stage
6. Append ToolEvent      — persist to SessionState.Events
7. DetectChain()         — scan event window for attack sequences
8. if chain detected     — Add(0.35) to risk
9. Persist updated state — write back to store
10. Risk gate            — return PolicyViolation if score ≥ 0.7 or ≥ 0.9
```

---

### 3.6 `pkg/agent/fleet.go` — FleetManager

```
FleetManager
    Name() string                                          — "agent_fleet"
    Validate(ctx, req) *PolicyViolation                    — admission.Validator
    getOrCreate(sessionID) *SessionAgent                   — double-checked locking
```

`FleetManager` is a `sync.RWMutex`-protected map of `SessionAgent` values.
Session creation uses double-checked locking (read lock → miss → write lock → re-check).

**Wiring**: `FleetManager` is appended to the `validators` slice in `buildInterceptor()` in `cmd/main.go`
when `agentFleet.enabled = true` in `policies.yaml`. No other changes to the admission engine are needed.

---

### 3.7 `pkg/llm/ollama.go` — Local Ollama Client

A local LLM client for offline / air-gapped environments.
Implements the same `llm.Client` interface as the Anthropic Claude client.

```
NewOllamaClient(baseURL string, opts ...OllamaOption) Client
```

Uses Ollama's `/api/chat` endpoint with `stream: false`.
Default model: `llama3`. Swap via `WithOllamaModel("mistral")`.

**Current use**: not wired into any validator yet.
This is the client that Phase 4 intent seeding (local context integration) will use
for cold-start embedding and local inference when the Anthropic API is not available.

---

## 4. Configuration

In `configs/policies.yaml`:

```yaml
agentFleet:
  enabled: false              # set true to activate
  driftThreshold: 0.4         # 0.0–1.0; lower = stricter
  behaviorWindowSize: 10      # events scanned for attack chains
  attackChainEnabled: true    # toggle sequence detection
```

All knobs are runtime-configurable — no recompile needed. In Kubernetes,
update the `ConfigMap` and the pod picks it up on next policy reload.

---

## 5. Risk Contribution Table

| Signal | Contribution | Threshold |
|--------|-------------|-----------|
| Intent drift (per call) | `drift × 0.3` | drift > `driftThreshold` |
| Attack chain detected | `+0.35` (flat) | any known chain in window |
| **HUMAN_REVIEW fires** | — | score ≥ 0.70 |
| **TERMINATE fires** | — | score ≥ 0.90 |

A session that drifts hard (drift = 0.8) on a single call contributes 0.24.
A full recon→exploit→exfiltrate chain adds 0.35.
Combined: 0.59 — below the review threshold on its own, but a second drifting call
would push the session over 0.70.
The thresholds are deliberately conservative for the dev spike.

---

## 6. What Is Not Here Yet (Phase 4)

- **Local context seeding**: at session start, call the context provider,
  parse the PRD problem statement, and use it as the intent baseline instead of waiting
  for the first observed user message. See LOCAL-CONTEXT-INTEGRATION.md (TBD).
- **Governance event write-back**: when PerchGuard terminates a session, write a
  snapshot entry back into the project context store so the mission record reflects the outcome.
- **Redis SessionStore**: the `store.SessionStore` interface is ready; the Redis
  implementation is a drop-in swap for multi-instance deployments.
- **FleetManager eviction**: sessions currently live until process restart.
  A TTL-based eviction loop is the next addition to `fleet.go`.
- **Embedding via Ollama**: the `ollamaClient` is wired and tested; switching
  `IntentModel` from word-frequency to neural embeddings is a one-file change.

---

## 7. The Closed Loop (Why This Matters)

```
Mission declared (project context)
        ↓
Agent governed against that mission (PerchGuard — this phase)
        ↓
Outcome recorded back (Snapshot — Phase 4)
```

Phase 3 is the second node in the closed loop.
It is the first time PerchGuard can say: *"this agent has drifted from its declared mission"*
rather than just *"this specific tool call looks suspicious."*

That is the shift from reactive interception to proactive governance.
