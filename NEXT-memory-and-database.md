# NEXT: Memory & Database Layer for the LangGraph Agent

North star for the next phase of work: stop extending PerchGuard's own governance
features and instead make `deployments/python-agents/insurance-agent-langgraph/`
a credible, production-shaped agentic deployment — checkpointing, conversation/
state memory, and a real database — with PerchGuard continuing to govern every
hop as it already does.

## Why this, why now

PerchGuard is the differentiator (governed multi-agent delegation for regulated
domains). It is not a substitute for the memory/database substrate every agentic
system needs regardless of governance. Right now that substrate doesn't exist:

- `graph.py`'s `build_graph()` compiles with **no checkpointer** — confirmed in
  `graph.py`'s last line and called out explicitly in
  `sdk/python/perchguard/langgraph_adapter.py`'s module docstring. Every
  `graph.invoke()` starts from a blank `ClaimReviewState()`; nothing persists
  between runs, and a crash mid-graph loses everything.
- There is no database in the deployment at all — no Postgres, no Redis, no
  vector store. PerchGuard's own SQLite audit.db records *governance decisions*
  (tool calls, ALLOW/DENY), not agent conversation state or business data.
- The mock MCP server's `run_sql` tool talks to an in-memory/mock claims store,
  not a real persisted database.

## Why the LangGraph checkpointer isn't just a drop-in

`langgraph_adapter.py` already documents the blocker: LangGraph's
checkpoint/resume/replay would re-run a node's PerchGuard `register()`/`evict()`
side effects on replay — double registration, budget double-spend, or an evict
racing a resumed run. **Adding a checkpointer requires idempotency keys
server-side first** (e.g. dedupe `register` calls by a client-supplied run+node
ID). This is real design work, not config.

## Proposed scope, roughly in order

1. **Idempotent registration in PerchGuard** — accept an idempotency key on
   `POST /agents/register` so a replayed node doesn't double-register or
   double-spend budget. Needed before checkpointing is safe to turn on.
2. **LangGraph checkpointer** — wire `SqliteSaver` or `PostgresSaver` into
   `build_graph()` once (1) lands, so a crashed/killed graph run can resume
   from its last completed node instead of restarting from `intake`.
3. **Real database for the domain data** — replace the mock MCP server's
   in-memory claims/policy data with Postgres, so `run_sql`, `read_policy`,
   `search_claims` hit a real schema instead of hardcoded fixtures.
4. **Conversation/session memory layer** — a place for prior claim reviews,
   past decisions, and retrieved precedent to live and be retrieved
   (pgvector or a dedicated vector store) so agents like `senior_approver` can
   reference similar past claims instead of only seeing the current run's
   state.
5. **Re-evaluate budget pooling across concurrent siblings** — flagged in
   `langgraph_adapter.py` as a known gap (each of `investigator`/`compliance`
   gets its own fraction of the parent's budget independently); worth
   revisiting once real persistence makes multi-run cost tracking meaningful.

## Explicitly not doing next

- No new PerchGuard policy/validator features.
- No new agent roles or graph nodes.
- No dashboard/UI work.

## Open questions to resolve before implementation starts

- Sqlite (simple, single-writer, fine for demo) vs. Postgres (matches "real
  shop" positioning, needed anyway for claims data) for the checkpointer —
  leaning Postgres since we need it for claims data regardless.
- Where session eviction hygiene for `sets_parent_for_children=True` nodes
  (currently left registered indefinitely per `langgraph_adapter.py`) fits
  once checkpointing exists — a TTL/reaper becomes more important once runs
  can be resumed hours later.
