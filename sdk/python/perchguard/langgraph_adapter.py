"""LangGraph adapter — runs LangGraph nodes as governed PerchGuard sessions.

Governance itself (session lifecycle, delegation scope, token verification) stays
entirely in PerchGuardClient / GovernedAgentLoop and the Go server
(pkg/api/agents.go). LangGraph owns orchestration only — which node runs when,
fan-out/fan-in shape — it never decides who is authorized to register as whose
child. That boundary has to be enforced server-side (parent-token verification via
manifest.Store.Verify), not trusted from graph state, because graph state is
mutable and shared across nodes: a node cannot prove who actually invoked it just
by reading a field another node wrote.

`governed_node()` reads `state[parent_key]` (a GovernanceParent: session_id +
token) to register as a delegated child, but a node only ever *writes*
`parent_key` when it's explicitly built with `sets_parent_for_children=True` — the
node's own children read it next. Two nodes that run concurrently (a LangGraph
fan-out) must both leave `sets_parent_for_children=False`, so neither writes that
state key: LangGraph's default reducer rejects two concurrent writes to the same
key in one super-step (raises `InvalidUpdateError`), so a graph that mis-wires this
fails closed rather than silently merging two candidate parents.

Scope boundaries (deliberate, not oversights — see PHASE10-11-LIVE-DEMO-RUNBOOK.md
for the live-validation pass this still needs):

- No checkpointer is wired into any StateGraph built with this adapter
  (`graph.compile()` with no `checkpointer=`). LangGraph's checkpoint/resume/replay
  would re-run a node's register/evict side effects against a server with no idea a
  replay happened — double registration, budget double-spend, or an evict racing a
  resumed run. Closing that needs idempotency keys server-side. Don't add a
  checkpointer to a graph built from this adapter until that's solved.
- No budget pooling across concurrent siblings. Each child registered under the
  same parent independently receives `delegationFraction * parent's own limit`
  (pkg/api/agents.go's existing budget-inheritance logic, unchanged) — two or more
  concurrent children can collectively consume more than the parent's nominal
  budget. See test_langgraph_adapter.py's budget test for the current behavior.
- Nodes built with `sets_parent_for_children=True` are not evicted by `governed_node`
  on success — see that function's docstring for why (evicting before children
  register against that session would break their parent-token proof). Those
  sessions stay registered until something else cleans them up. Don't rely on
  `governed_node` alone for full session lifecycle hygiene in a graph that uses
  fan-out/fan-in.
"""

from __future__ import annotations

from typing import Any, Callable, Optional, TypedDict

from .client import PerchGuardClient
from .loop import GovernedAgentLoop


class GovernanceParent(TypedDict):
    session_id: str
    token: str


def governed_node(
    *,
    agent_id: str,
    agent_role: str,
    pg: PerchGuardClient,
    loop: GovernedAgentLoop,
    task: Callable[[dict], str],
    tools: list[dict],
    result_key: str,
    parent_key: str = "governance_parent",
    sets_parent_for_children: bool = False,
) -> Callable[[dict], dict]:
    """Build a LangGraph node function that runs `task(state)` under its own
    governed PerchGuard session.

    Reads `state[parent_key]` (if present) to register as that session's
    delegated child, proving it via the parent's own token — see module
    docstring. Registers and runs the governed loop.

    A node that raises mid-run evicts (best-effort) before re-raising, so it
    never leaks a registered-but-never-evicted session. A node that succeeds
    and is NOT `sets_parent_for_children` also evicts immediately afterward.
    A node that succeeds and IS `sets_parent_for_children` deliberately does
    NOT evict here: its session_id/token are handed to children in the
    returned state, and PerchGuard's DELETE /api/sessions/{id} really does
    remove the registration (pkg/api/sessions.go) — evicting before those
    children register would make their parent-token proof fail server-side
    with "parent_session_id not found or expired". That session is left
    registered until the graph run's caller cleans it up (e.g. via TTL/reaper
    on the server side); this adapter has no "this subtree is done" hook to
    evict it sooner.
    """

    def node(state: dict) -> dict:
        parent: Optional[GovernanceParent] = state.get(parent_key)
        task_text = task(state)
        session = pg.register(
            agent_id=agent_id,
            agent_role=agent_role,
            intent=task_text,
            parent_session_id=parent["session_id"] if parent else "",
            parent_token=parent["token"] if parent else None,
        )
        try:
            result = loop.run(
                task=task_text,
                agent_id=agent_id,
                agent_role=agent_role,
                tools=tools,
                session=session,
            )
        except Exception:
            pg.evict(session, best_effort=True)
            raise

        update: dict[str, Any] = {result_key: result}
        if sets_parent_for_children:
            update[parent_key] = GovernanceParent(session_id=session.id, token=session.token)
        else:
            pg.evict(session, best_effort=True)
        return update

    return node
