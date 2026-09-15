# Phase 12 — Swarm Graph (Topology + Cross-Agent Data Lineage)

Status: Spec only, not implemented.

## Goal

One graph view of the live agent fleet: delegation structure (who spawned
whom) and data lineage (what data actually moved between agents) — reusing
existing stores, no new external dependencies.

## What already exists (no changes needed)

- Delegation edges: `DelegationStore.parents` (`pkg/agent/delegation.go:16`)
  — authoritative parent→child map, with cycle-safe `Depth()`.
- Per-node risk/drift: `SessionSummary` (`pkg/audit/sink.go:27`) — risk,
  drift, steps, per session.
- Per-session data lineage: `LineageGraph` (`pkg/store/lineage.go`) —
  `outRef → inRefs, tool` edges, built by the exfiltration validator
  (`pkg/admission/validator/lineage.go:53`) on every call.

## The actual gap

`LineageStore.graphs` is keyed by `sessionID` — a child agent's lineage
graph starts empty. If a parent hands a sub-agent a `DataRefOut` value and
the sub-agent immediately sends it externally, `HasAny` in the child's own
graph won't see it — the exfiltration validator can't catch it. This is a
real bypass in the trust boundary the delegation feature introduces, not
just a demo cosmetic. Fixing it is what makes the graph "graph-native"
instead of a UI layer on top of disconnected data.

## Design

### 1. Manifest: declare inherited refs (`pkg/manifest/manifest.go`)

Add `InheritedDataRefs []string` next to `ParentSessionID`
(`pkg/manifest/manifest.go:26`). The delegating agent declares which of its
own `DataRefOut` values it is handing to the sub-agent — same
declared-intent philosophy as the rest of the manifest; nothing inferred.

### 2. Verify, don't trust (`pkg/api/agents.go:63-85`)

Same pattern already used for `ParentSessionID` (token verified via
`manifestStore.Verify`, `agents.go:74`): before seeding the child's lineage
graph, check each claimed ref actually exists in the parent's
`LineageGraph` (`ls.Graph(parentSessionID).tools[ref]`). Reject
registration if a claimed ref wasn't actually produced by the parent —
otherwise a sub-agent could claim arbitrary lineage to launder data through
the graph.

**Open decision:** verification checks only the direct parent's graph, not
the whole ancestor chain — a grandchild can't claim a grandparent's ref
unless it is re-declared at each hop. This matches how `ValidateScope`
already treats delegation one hop at a time (`pkg/agent/delegation.go:73`).
Revisit if a use case needs transitive lookup.

### 3. `LineageStore`: cross-session seed (`pkg/store/lineage.go`)

Add `SeedInherited(childSessionID string, refs []string, originSessionID
string)` — copies verified refs into the child's graph tagged with their
true origin session+tool. Requires an `origin map[string]string` alongside
`tools`/`edges` in `LineageGraph` (small additive change). This is what
lets both the exfiltration validator work across the delegation boundary
*and* the graph endpoint draw a real cross-agent data edge.

### 4. New endpoint: `GET /api/swarm`

Auth-gated via `requireAPIKey` like every other `/api/*` route
(`pkg/api/server.go:81` pattern). Aggregates:

```json
{
  "nodes": [{"session_id","agent_id","risk_score","drift_score","parent_session_id"}],
  "edges": [
    {"type":"delegation","from":"<parent>","to":"<child>"},
    {"type":"data","from_session":"...","from_tool":"...","to_session":"...","to_tool":"...","ref":"..."}
  ]
}
```

Built from `DelegationStore` + `LineageStore` + existing session list — no
audit schema changes, no new persisted state beyond the additive `origin`
map.

### 5. Frontend: swarm panel in `pkg/api/dashboard.go`

Same inline vanilla-JS/CSS style already used there (no framework, no CDN
dependency) — a small dependency-free force-directed layout (~100 lines),
nodes colored by risk bucket reusing the existing `.green/.yellow/.red`
classes, solid edges for delegation, dashed for data lineage, click a node
to pull its `Steps` trace from `/api/sessions/{id}`.

## Why this is wiring, not a new subsystem

`DelegationStore` and `LineageStore` already exist and already do the hard
part; the only genuinely new logic is the inherited-refs verification step,
which is a handful of lines next to code that already does the equivalent
check for tokens.

## Before merge (per repo checklist)

- Touches agent input + a trust boundary → `/security-review` is
  mandatory, given the "verify claimed refs" step is exactly the kind of
  thing that's easy to get subtly wrong (e.g. accepting refs from a
  grandparent session, not just the direct parent).
- `go test -race ./...` — `LineageStore`/`LineageGraph` already use
  mutexes; the new `origin` map needs the same discipline.
- No new deps, no audit schema change.

## Implementation order (when approved)

1. Manifest field (`InheritedDataRefs`)
2. Lineage store verify + seed (`SeedInherited`, `origin` map)
3. `GET /api/swarm` endpoint
4. Dashboard swarm panel
5. Tests (unverified-ref rejection, endpoint shape, race detector)
