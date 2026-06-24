# PerchGuard Phase 4 — Operability Platform: User Manual

**Date**: 2026-04-29  
**Branch**: `phase4/operability-platform`  
**Go version**: 1.25

---

## What Was Built

Phase 4 adds five capabilities on top of the existing admission pipeline:

| # | Capability | Key files |
|---|-----------|-----------|
| 1 | Management REST API (7 endpoints) | `pkg/api/` |
| 2 | Audit ring buffer (in-memory, live query) | `pkg/store/audit.go` |
| 3 | LLM governance query (`POST /api/query`) | `pkg/api/query.go` |
| 4 | Per-validator child spans in Jaeger | `pkg/admission/interceptor.go` |
| 5 | Policy hot-reload (30-second polling) | `pkg/policy/watcher.go` |

No new external dependencies were added. The API runs on the existing `:8080` listener.

---

## File-by-File Walk-through

### `pkg/store/audit.go` — Audit Ring Buffer

**Purpose**: Give the API a queryable in-memory trail of the last 1000 admission decisions.

```
AuditRecord        — one admission decision (request_uid, session_id, tool_name, decision, reason, duration_ms …)
AuditFilter        — Decision / ToolName / SessionID / Limit (default 100, max 1000)
AuditRingBuffer    — circular buffer, capacity 1000, protected by sync.RWMutex
  NewAuditRingBuffer(capacity int)
  Push(AuditRecord)                   — overwrites oldest when full
  Query(AuditFilter) []AuditRecord    — newest-first, filtered
```

> **Why a string `Decision` field?**  
> `AuditRecord.Decision` is a plain `string`, not `admission.Decision`. This breaks the import cycle:  
> `pkg/store` cannot import `pkg/admission` (admission already imports store).  
> The translation (`admission.AuditEntry` → `store.AuditRecord`) happens in `pkg/telemetry/otel.go`, which already imports both.

---

### `pkg/store/memory.go` — `List()` added to `SessionStore`

`SessionStore` interface gained one method:

```go
List() []*SessionState
```

`MemoryStore` implements it with a read lock — returns a snapshot of all active sessions. Called by:
- `GET /api/sessions`
- `GET /api/stats`
- `POST /api/query` (context building)

---

### `pkg/policy/watcher.go` — Policy Hot-Reload

**Purpose**: Let an operator edit `configs/policies.yaml` and have the change take effect within 30 seconds, without restarting the container.

```
LoadResult         — wraps Config + SHA-256 hash (first 8 bytes, hex) + LoadedAt + Path
LoadWithMeta(path) — drop-in for Load(); returns LoadResult
Watcher            — polls file on a ticker
  NewWatcher(path, interval, onReload func(*LoadResult))
  Start(initialHash)   — launches background goroutine
  Stop()               — closes stop channel
```

**Flow on each tick**:
1. `os.ReadFile` → SHA-256 → compare with `lastHash`
2. If hash unchanged → skip
3. If hash changed → `LoadWithMeta` → call `onReload(result)` → update `lastHash`
4. On parse error → log warning, keep current policy

Uses `time.Ticker` and a `chan struct{}` stop signal. Zero new dependencies (no fsnotify).

---

### `pkg/admission/interceptor.go` — Mutex, Reload, Names, Child Spans

Four additions:

#### 1. `sync.RWMutex mu`
Protects the `validators`, `mutators`, `quotas` slices.  
`Intercept()` takes a **read lock only long enough to copy slice references** — actual validation work runs outside the lock.

#### 2. `ReloadValidators(validators, mutators, quotas []…)`
Called by the policy watcher. Acquires write lock → swaps the three slices atomically. Ongoing requests are unaffected because they already hold a copy of the old slice references.

#### 3. Name methods
```go
interceptor.ValidatorNames() []string
interceptor.MutatorNames()   []string
interceptor.QuotaNames()     []string
```
Used by `GET /api/pipeline` to report what is currently active.

#### 4. Per-validator child spans
Every quota, validator, and mutator call is wrapped in a child OpenTelemetry span:

```
intercept (203ms)
  ├── quota.depth_limiter        (0ms)
  ├── quota.session_budget       (0ms)
  ├── validator.prompt_injection  (0ms)
  ├── validator.tool_auth         (0ms)
  ├── validator.data_exfil        (0ms)
  ├── validator.semantic_firewall (201ms)  ← LLM call
  ├── validator.agent_fleet       (1ms)
  ├── mutator.parameter_sanitizer (0ms)
  ├── mutator.least_privilege     (0ms)
  └── decision: ALLOW
```

DENY/TERMINATE spans are marked `codes.Error` → Jaeger highlights them red.

---

### `pkg/telemetry/otel.go` — `TeeAuditLogger`

**Purpose**: Dual-write audit entries — stdout JSON trail (durable) AND ring buffer (live API).

```go
type TeeAuditLogger struct {
    inner *AuditLogger        // existing stdout logger
    ring  *store.AuditRingBuffer
}

func (t *TeeAuditLogger) Log(entry admission.AuditEntry) {
    t.inner.Log(entry)        // → stdout
    t.ring.Push(store.AuditRecord{ … })  // field copy (avoids import cycle)
}
```

`TeeAuditLogger` implements `admission.AuditLogger` — it is a drop-in replacement.  
`AuditLogger` is unchanged; `TeeAuditLogger` wraps it.

---

### `pkg/api/middleware.go` — JSON helpers

Two package-private helpers used by every handler:

```go
writeJSON(w, status int, v any)          // sets Content-Type, status, encodes v
writeError(w, status int, msg string)    // {"error": "msg"}
```

---

### `pkg/api/server.go` — `APIServer` struct + routing

```go
type APIServer struct {
    sessions    store.SessionStore
    auditRing   *store.AuditRingBuffer
    interceptor *admission.Interceptor
    fleet       *agent.FleetManager
    llmClient   llm.Client
    policyMeta  *atomic.Pointer[policy.LoadResult]
}
```

`policyMeta` is an `atomic.Pointer` updated by the watcher in `cmd/main.go`. `GET /api/pipeline` reads it without a lock.

`RegisterRoutes(mux)` mounts all 7 endpoints using Go 1.22+ method+path syntax:

```
GET    /api/sessions
GET    /api/sessions/{id}
DELETE /api/sessions/{id}
GET    /api/audit
GET    /api/pipeline
GET    /api/stats
POST   /api/query
```

---

### `pkg/api/sessions.go` — Session endpoints

#### `GET /api/sessions`
1. `sessions.List()` → `[]*SessionState`
2. Project each to `sessionSummary{session_id, risk_score, event_count, created_at, updated_at}`
3. `writeJSON(200, []sessionSummary)`

#### `GET /api/sessions/{id}`
1. `r.PathValue("id")` → `sessions.Get(id)`
2. 404 if not found
3. Project `Events []ToolEvent` to `[]eventRecord{tool, stage, timestamp}`
4. `writeJSON(200, sessionDetail)`

#### `DELETE /api/sessions/{id}`
1. 404 if not found
2. `sessions.Delete(id)` — evict from store
3. `fleet.Evict(id)` — evict from `FleetManager.agents` map (closes the eviction gap)
4. `writeJSON(200, {session_id, status: "terminated"})`

---

### `pkg/api/audit.go` — `GET /api/audit`

Reads query params `decision`, `tool`, `session_id`, `limit` and builds an `AuditFilter`.  
Calls `auditRing.Query(filter)` → newest-first results.  
Returns `[]AuditRecord` (empty array `[]` when nothing matches, never `null`).

**Example**:
```bash
curl "http://localhost:8080/api/audit?decision=DENY&tool=bash&limit=10"
```

---

### `pkg/api/pipeline.go` — `GET /api/pipeline`

Reads `policyMeta.Load()` (atomic, no lock) and calls the three `*Names()` methods on `interceptor`.

Returns `PipelineStatus`:
```json
{
  "validators": ["prompt_injection", "tool_auth", "data_exfil", "semantic_firewall", "agent_fleet"],
  "mutators": ["parameter_sanitizer", "least_privilege"],
  "quotas": ["depth_limiter", "session_budget"],
  "semantic_firewall_enabled": true,
  "policy_hash": "a3f7b2c1",
  "policy_path": "./configs/policies.yaml",
  "loaded_at": "2026-04-29T10:00:00Z"
}
```

Use `policy_hash` to confirm a hot-reload took effect.

---

### `pkg/api/stats.go` — `GET /api/stats`

1. `sessions.List()` → total session count
2. `auditRing.Query(Limit: 1000)` → aggregate decision counts + denied-tool frequency
3. `topN(deniedTools, 5)` → top 5 denied tools, sorted descending

Returns `Stats`:
```json
{
  "total_sessions": 3,
  "decisions": {"ALLOW": 142, "DENY": 8, "MUTATE": 2},
  "top_denied_tools": [
    {"tool": "bash", "count": 5},
    {"tool": "read_file", "count": 3}
  ]
}
```

---

### `pkg/api/query.go` — `POST /api/query`

**Purpose**: Let an operator ask a plain-English governance question answered by Claude Haiku using live ring buffer data.

**Flow**:
1. Check `llmClient != nil` → else `503 {"error": "LLM not configured"}`
2. Decode `{"question": "…"}` from body
3. `auditRing.Query(Limit: 50)` + `sessions.List()` → JSON-encode as context
4. Cap context string at **3000 chars** (Haiku token budget protection)
5. Build `userMessage = "Context:\n{ctx}\n\nQuestion: {q}"`
6. `context.WithTimeout(r.Context(), 10*time.Second)` — overrides the semantic firewall's 180ms budget
7. `llmClient.Complete(ctx, CompletionRequest{SystemPrompt: governanceSystemPrompt, …})`
8. Return `QueryResponse{answer, input_tokens, output_tokens, duration_ms}`

The `governanceSystemPrompt` frames the assistant as a governance tool — speaks in agents, sessions, tool calls, decisions. Domain-agnostic (not insurance-specific).

---

### `cmd/main.go` — Final Wiring

Key changes in `main()`:

#### Before Phase 4
```go
memStore → created inside buildInterceptor(), invisible to the outside
auditLog → plain AuditLogger writing to stdout
policy   → Load(), hash not tracked
```

#### After Phase 4
```go
// 1. Store and ring buffer created in main() scope
memStore  := store.NewMemoryStore()
auditRing := store.NewAuditRingBuffer(1000)
auditLog  := telemetry.NewTeeAuditLogger(telemetry.NewAuditLogger(cfg.Policies.Audit), auditRing)

// 2. Policy loaded with metadata
policyResult, _ := policy.LoadWithMeta(policyPath)
var currentPolicy atomic.Pointer[policy.LoadResult]
currentPolicy.Store(policyResult)

// 3. Pipeline slices extracted to their own function (hot-reload safe)
validators, mutators, quotas := buildPipelineSlices(cfg, llmClient, fleetMgr)
interceptor := admission.NewInterceptor(validators, mutators, quotas, auditLog, opts...)

// 4. Policy watcher — hot-reload on hash change
watcher := policy.NewWatcher(policyPath, 30*time.Second, func(result *policy.LoadResult) {
    newV, newM, newQ := buildPipelineSlices(result.Config, llmClient, fleetMgr)
    interceptor.ReloadValidators(newV, newM, newQ)
    currentPolicy.Store(result)
})
watcher.Start(policyResult.Hash)
defer watcher.Stop()

// 5. API server created and routes registered
apiServer := api.NewAPIServer(memStore, auditRing, interceptor, fleetMgr, llmClient, &currentPolicy)
// ...
apiServer.RegisterRoutes(mux)
```

`buildPipelineSlices` is a standalone function (not a method) so both `main()` and the watcher callback can call it identically. The fleet manager, interceptor, and store are **never** reconstructed on hot-reload — only the validator/mutator/quota slices swap.

---

## Data Flow Diagram

```
HTTP Request → POST /intercept
       │
       ▼
  Interceptor.Intercept(ctx, req)
       │
       ├─ [RLock] snapshot validators, mutators, quotas [RUnlock]
       │
       ├─► quota.depth_limiter    ──child span──► TERMINATE if over depth
       ├─► quota.session_budget   ──child span──► TERMINATE if over budget
       │
       ├─► validator.prompt_injection  ──child span──► violation?
       ├─► validator.tool_auth         ──child span──► violation?
       ├─► validator.data_exfil        ──child span──► violation?
       ├─► validator.semantic_firewall ──child span──► Claude Haiku (180ms budget)
       ├─► validator.agent_fleet       ──child span──► drift/attack-chain check
       │
       ├─► mutator.parameter_sanitizer ──child span
       ├─► mutator.least_privilege     ──child span
       │
       ├─► auditLog.Log(entry)
       │       ├─► stdout JSON              (AuditLogger)
       │       └─► auditRing.Push(record)   (TeeAuditLogger → ring buffer)
       │
       └─► Response: ALLOW | DENY | MUTATE | HUMAN_REVIEW | TERMINATE


Policy Watcher (background goroutine, every 30s)
       │
       ├─ stat + SHA-256 policies.yaml
       ├─ hash unchanged → skip
       └─ hash changed → LoadWithMeta → buildPipelineSlices → interceptor.ReloadValidators
                                                             → currentPolicy.Store(result)
```

---

## All API Calls — Quick Reference

```bash
# Health check (pre-existing)
curl http://localhost:8080/healthz

# List all active sessions
curl http://localhost:8080/api/sessions | jq

# Get one session with full event history
curl http://localhost:8080/api/sessions/{session_id} | jq

# Terminate a session immediately
curl -X DELETE http://localhost:8080/api/sessions/{session_id} | jq

# Query recent decisions (all filters optional)
curl "http://localhost:8080/api/audit" | jq
curl "http://localhost:8080/api/audit?decision=DENY" | jq
curl "http://localhost:8080/api/audit?tool=bash&limit=10" | jq
curl "http://localhost:8080/api/audit?session_id=abc123" | jq

# See active validators, mutators, quotas, and current policy hash
curl http://localhost:8080/api/pipeline | jq

# Aggregate stats: total sessions, decision distribution, top denied tools
curl http://localhost:8080/api/stats | jq

# Ask a natural language governance question (requires ANTHROPIC_API_KEY)
curl -X POST http://localhost:8080/api/query \
  -H "Content-Type: application/json" \
  -d '{"question": "which sessions look most risky right now?"}' | jq

# Confirm a hot-reload happened (edit configs/policies.yaml, wait ≤30s)
curl http://localhost:8080/api/pipeline | jq .policy_hash
```

---

## Error Responses

All errors use `{"error": "message"}`.

| Scenario | Status |
|---------|--------|
| Session not found | `404` |
| LLM not configured (no API key) | `503` |
| Malformed JSON body on `POST /api/query` | `400` |
| LLM call error or timeout | `500` |

---

## What Phase 4 Does NOT Include

- Authentication on the management API (Phase 5)
- Persistent audit store (SQLite / Redis)
- Prometheus `/metrics` endpoint
- Dashboard UI
- Agent identity
