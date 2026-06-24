# PerchGuard — Local Context Integration: Functional Manual

> **Branch:** `phase-copidock-integration` _(branch name preserved for git history)_
> **Shipped in:** `d7f4348` — declared-intent governance loop
> **Refactored in:** `c1a26cc` — absorbed into `pkg/localfs`, removed external tool dependency
> **Date:** 2026-05-05

---

## 1. Overview

This integration closes the feedback loop between a project's **local context store** and **PerchGuard**. Before this work, PerchGuard inferred agent intent at runtime from the live call stream. Now an agent declares its identity and mission upfront via an **AgentManifest**, and PerchGuard enriches that declaration with PRD + snapshot context from the local `context/` directory — giving the risk scorer a richer, more stable baseline from the first tool call.

```
Agent                  PerchGuard                     Local context/ directory
 │                         │                                    │
 │── POST /agents/register ─▶                                   │
 │      (AgentManifest)    │── ReadContext(limit=3) ───────────▶│
 │                         │◀─────── PRD + snapshots ───────────│
 │                         │  seed SessionAgent intent           │
 │                         │  register in manifest.Store         │
 │◀─── {sessionID, token} ─│                                     │
 │                         │                                     │
 │── /intercept + token ──▶│ verify token → ALLOW / DENY        │
 │                         │                                     │
 │── DELETE /api/sessions ─▶                                     │
 │                         │── EmitAsync(GovernanceRecord) ─────▶│
 │                         │              context/snapshots/     │
 │                         │              entries.json           │
```

**Standalone mode** (no `context/` directory): `AgentManifest` alone provides the intent baseline. Governance records are written to `./snapshots/governance.json` instead.

---

## 2. Prerequisites

| Requirement | Notes |
|-------------|-------|
| Go 1.22+ | Module path: `github.com/Droshow/PerchGuard/perchguard` |
| `context/` project directory | Optional. Must contain `context/snapshots/entries.json` and optionally `context/prds/` |
| `PERCHGUARD_CONTEXT_ROOT` env var | Optional — if unset PerchGuard auto-discovers by walking up from CWD |

If no `context/` directory is found, PerchGuard runs in standalone mode: manifest-only intent, `FileSink` audit output.

---

## 3. Agent Manifest

### 3.1 Format

An AgentManifest is submitted as **YAML** (default) or **JSON** at registration time. The parser auto-detects format: payloads starting with `{` are treated as JSON; everything else as YAML.

**Minimal valid manifest (YAML):**

```yaml
apiVersion: perchguard.io/v1alpha1
kind: AgentManifest
metadata:
  id: my-agent-001
  owner: team-platform
  created: "2026-05-01"
  version: "1.0.0"
mission:
  summary: "Reads Kubernetes resource metadata to produce cost reports. Read-only."
  scope:
    - kubernetes resource listing
    - cost allocation tagging
  out_of_scope:
    - cluster mutations
    - credential access
authorization:
  role: readonly-observer
  allowed_systems:
    - kubernetes-api
  human_review_required_for:
    - any write operation
invariants:
  - Never write to the cluster
  - Never exfiltrate secrets
project_context:
  project_id: k8s-cost-reporter
  prd_version: "v0.3"
  adr_refs:
    - ADR-005
```

### 3.2 Required Fields

| Field | Rule |
|-------|------|
| `metadata.id` | Non-empty string — globally unique agent identifier |
| `metadata.version` | Non-empty string — used in governance records |
| `mission.summary` | Non-empty string — primary intent text for drift scoring |

All other fields are optional but strongly recommended for meaningful governance.

### 3.3 Intent Text Derivation

`AgentManifest.IntentText()` returns the string used to seed the `SessionAgent` baseline:

```
<mission.summary> <scope[0]> <scope[1]> ...
```

This is then concatenated with context from `audit.ContextProvider` (if loaded) before seeding.

---

## 4. API Reference

### 4.1 POST /agents/register

Registers an agent and returns a session token.

**Request**

```
POST /agents/register
Content-Type: application/yaml   (or application/json)

<AgentManifest body>
```

**Response 201 Created**

```json
{
  "agent_id": "my-agent-001",
  "manifest_version": "1.0.0",
  "session_id": "pg-my-agent-001-3f8a12b4e901",
  "token": "pgat-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
  "effective_policies": ["tool-risk", "intent-drift", "data-sensitivity"],
  "context_loaded": true
}
```

| Field | Description |
|-------|-------------|
| `session_id` | Use as `X-Session-ID` on subsequent `/intercept` calls |
| `token` | Use as `X-PerchGuard-Agent-Token` on `/intercept` calls |
| `context_loaded` | `true` when a `context/` root was found and enrichment was read |

**Error responses**

| Code | Reason |
|------|--------|
| 400 | Manifest parse/validation failed — check `error` field |
| 500 | Token or session-ID generation failed (crypto error) |

---

### 4.2 GET /agents/{session_id}

Returns the current registration details for a session.

**Response 200 OK**

```json
{
  "agent_id": "my-agent-001",
  "manifest_version": "1.0.0",
  "session_id": "pg-my-agent-001-3f8a12b4e901",
  "registered_at": "2026-05-05T12:00:00Z",
  "mission_summary": "Reads Kubernetes resource metadata to produce cost reports.",
  "scope": ["kubernetes resource listing", "cost allocation tagging"],
  "auth_role": "readonly-observer"
}
```

---

### 4.3 POST /intercept (token-aware)

Token verification is layered on top of the existing intercept pipeline. The token header is **optional and backward-compatible** — agents that never registered continue to work without a token.

**Headers**

```
X-Session-ID: pg-my-agent-001-3f8a12b4e901
X-Agent-ID:   my-agent-001
X-PerchGuard-Agent-Token: pgat-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4
```

When the token header is present but invalid:

```json
{
  "verdict": "DENY",
  "reason": "unregistered_agent: invalid or expired session token"
}
```

---

### 4.4 DELETE /api/sessions/{id} (governance write-back)

Evicting a session emits a `GovernanceRecord` to the configured `audit.Sink` before the session is removed from memory.

**What is written:**

```json
{
  "id": "snap-20260505-120123",
  "timestamp": "2026-05-05T12:01:23Z",
  "summary": "Governance session: my-agent-001 v1.0.0 — 14 calls, peak risk 0.78",
  "source": "perchguard",
  "session_context": {
    "agent_id": "my-agent-001",
    "manifest_version": "1.0.0",
    "session_id": "pg-my-agent-001-3f8a12b4e901",
    "started": "2026-05-05T12:00:00Z",
    "ended": "2026-05-05T12:01:23Z",
    "tool_calls": 14,
    "decisions": {"ALLOW": 12, "DENY": 2},
    "peak_risk_score": 0.78,
    "terminated_early": false,
    "governance_events": [...]
  }
}
```

The write is **asynchronous** (`EmitAsync`) — never blocks the HTTP response. Appended atomically via tmp-file rename.

---

## 5. Local Context (pkg/localfs)

### 5.1 Root Discovery

The integration is **local-first** — no network calls. Root resolution order:

1. `PERCHGUARD_CONTEXT_ROOT` environment variable (explicit override)
2. Auto-discovery: walk up from CWD looking for a directory containing `context/snapshots/`

```bash
export PERCHGUARD_CONTEXT_ROOT=/home/dev/Work/my-project
```

Startup log when found:

```
[perchguard] local context active (root=/home/dev/Work/my-project)
```

Startup log when not found (standalone mode):

```
[perchguard] no context root found — governance records → ./snapshots/governance.json
```

### 5.2 Context Loading (Reader)

`Reader.ReadContext(limit int)` loads:

- **Latest PRD** — alphabetically last `.md` file under `context/prds/`, truncated to 800 chars
- **Recent human snapshots** — last `limit` entries from `context/snapshots/entries.json` where `source != "perchguard"` (PerchGuard-emitted entries are excluded to prevent circular intent feedback)

The combined text is appended to the manifest's `IntentText()` before seeding the `SessionAgent`.

### 5.3 Governance Write-Back (Writer)

`Writer.EmitAsync(record GovernanceRecord)` appends a record to `context/snapshots/entries.json` using an atomic write pattern:

1. Read current `entries.json`
2. Append the new record
3. Write to `entries.json.tmp`
4. Rename `tmp` → `entries.json`

The `source` field is always `"perchguard"`, which the Reader uses to filter these out on subsequent context loads.

### 5.4 Standalone Sink (FileSink)

When no `context/` root is found, `audit.FileSink` writes governance records to `./snapshots/governance.json` using the same atomic append pattern. This is the default for enterprise deployments without a local context store.

---

## 6. Token Lifecycle

```
POST /agents/register
  └─ manifest.IssueToken()           → "pgat-" + 32 hex chars (16 random bytes)
  └─ manifest.NewSessionID(agentID)  → "pg-" + agentID[:20] + "-" + 12 hex chars

/intercept (X-PerchGuard-Agent-Token present)
  └─ manifest.Store.Verify(sessionID, agentID, token)
       ├─ true  → proceed to policy evaluation
       └─ false → immediate DENY (reason: "unregistered_agent")

DELETE /api/sessions/{id}
  └─ manifest.Store.Delete(sessionID)  → token revoked
```

Tokens are **in-memory only** — a server restart clears all registrations. Agents must re-register after a restart.

---

## 7. Fleet Manager Integration (pkg/agent)

`FleetManager.SeedFromManifest` pre-creates a `SessionAgent` with the combined intent baseline before the first tool call arrives:

```go
fleet.SeedFromManifest(sessionID, intentText, manifest.ID, manifest.Version)
```

This writes `ManifestID` and `ManifestVersion` into `SessionState` so the governance record can reference them. `SessionAgent.Evaluate` tracks `PeakRiskScore` across the full session lifetime for inclusion in the write-back record.

---

## 8. Configuration Reference

| Env Var | Default | Description |
|---------|---------|-------------|
| `PERCHGUARD_CONTEXT_ROOT` | _(auto-discover)_ | Explicit path to project context root |
| `PERCHGUARD_ADDR` | `:8080` | HTTP listen port |

---

## 9. Quickstart

```bash
# 1. Start PerchGuard with context root
export PERCHGUARD_CONTEXT_ROOT=/home/dev/Work/my-project
./perchguard

# 2. Register an agent
curl -s -X POST http://localhost:8080/agents/register \
  -H "Content-Type: application/yaml" \
  --data-binary @my-agent.yaml | jq .

# 3. Use the returned token on intercept calls
SESSION_ID="pg-my-agent-001-3f8a12b4e901"
TOKEN="pgat-..."

curl -s -X POST http://localhost:8080/intercept \
  -H "X-Session-ID: $SESSION_ID" \
  -H "X-Agent-ID: my-agent-001" \
  -H "X-PerchGuard-Agent-Token: $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tool":"kubectl_get","params":{"resource":"pods","namespace":"default"}}' | jq .

# 4. Evict session (triggers governance write-back)
curl -s -X DELETE http://localhost:8080/api/sessions/$SESSION_ID | jq .
```

---

## 10. Package Map

| Package | Purpose |
|---------|---------|
| `pkg/manifest` | AgentManifest struct, Parse/Validate, Store (registry), IssueToken, NewSessionID |
| `pkg/audit` | `Sink` + `ContextProvider` interfaces; `GovernanceRecord` types; `FileSink`; `NoOpSink`/`NoOpContextProvider` |
| `pkg/localfs` | `Reader` (PRD + snapshot loading, root discovery); `Writer` (atomic append to `context/snapshots/entries.json`) |
| `pkg/admission` | Interceptor with `TokenVerifier` interface and `WithTokenVerifier` option |
| `pkg/agent` | `FleetManager.SeedFromManifest`, `SessionAgent.PeakRiskScore` tracking |
| `pkg/api` | `POST /agents/register`, `GET /agents/{id}`, `DELETE /api/sessions/{id}` with write-back |
| `cmd/main.go` | Wires manifest.Store + localfs Reader/Writer (or FileSink fallback) + token verifier |

---

## 11. Related Documents

- [PRD-perchguard-declared-intent.md](PRD-perchguard-declared-intent.md)
- [PHASE3-AGENT-FLEET.md](PHASE3-AGENT-FLEET.md)
- [PHASE4-OPERABILITY-PLATFORM.md](PHASE4-OPERABILITY-PLATFORM.md)
- [ARCHITECTURE.md](ARCHITECTURE.md)
