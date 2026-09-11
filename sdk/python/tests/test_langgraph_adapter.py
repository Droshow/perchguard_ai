"""Tests for the LangGraph adapter (perchguard/langgraph_adapter.py).

Uses a real langgraph.graph.StateGraph (not a mock of LangGraph) for the
determinism and fail-closed tests — the property being proven is about how the
real engine's default state reducer behaves under concurrent writes, which a
mocked graph can't demonstrate.
"""

from typing import TypedDict

import pytest
from langgraph.errors import InvalidUpdateError
from langgraph.graph import END, START, StateGraph

from perchguard import GovernedAgentLoop, Session
from perchguard.exceptions import PerchGuardConfigError
from perchguard.langgraph_adapter import GovernanceParent, governed_node

from test_loop import FakeAnthropic, FakeBlock, FakeResponse


class DiamondState(TypedDict, total=False):
    governance_parent: GovernanceParent
    root_result: str
    a_result: str
    b_result: str
    join_result: str


class FakePGRegistry:
    """Stands in for the external PerchGuardClient governed_node registers/evicts
    through — distinct from GovernedAgentLoop's own internal client, matching how
    the real demo app uses them (see deployments/insurance-agent-langgraph/)."""

    def __init__(self):
        self.registered: list[dict] = []
        self.evicted: list[str] = []
        self._counter = 0

    def register(self, **kwargs):
        if kwargs.get("parent_session_id") and not kwargs.get("parent_token"):
            raise PerchGuardConfigError("parent_session_id without parent_token")
        self._counter += 1
        session = Session(
            id=f"pg-session-{self._counter}",
            agent_id=kwargs["agent_id"],
            agent_role=kwargs["agent_role"],
            manifest_version="0.1.0",
            token=f"pgat-token-{self._counter}",
            intent=kwargs["intent"],
            parent_session_id=kwargs.get("parent_session_id", ""),
        )
        self.registered.append(kwargs)
        return session

    def evict(self, session, best_effort=False):
        self.evicted.append(session.id)


def _end_turn_loop(text="done"):
    responses = [FakeResponse("end_turn", [FakeBlock("text", text=text)])]
    return GovernedAgentLoop(
        anthropic_client=FakeAnthropic(responses),
        perchguard_url="http://localhost:8080",
        mcp_url="http://localhost:8090",
        model="claude-haiku-4-5-20251001",
        verbose=False,
    )


# -- fan-out determinism -------------------------------------------------


def test_concurrent_siblings_register_against_same_parent():
    """Both concurrent branches must register with the same parent_session_id and
    parent_token — the root's — proving there's no merge ambiguity: siblings only
    ever read governance_parent, never write it (see module docstring)."""
    registry = FakePGRegistry()

    root_node = governed_node(
        agent_id="claims-intake", agent_role="claims_intake", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "intake task", tools=[], result_key="root_result",
        sets_parent_for_children=True,
    )
    a_node = governed_node(
        agent_id="fraud-investigator", agent_role="fraud_investigator", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "investigate", tools=[], result_key="a_result",
    )
    b_node = governed_node(
        agent_id="compliance-reviewer", agent_role="compliance_reviewer", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "review compliance", tools=[], result_key="b_result",
    )

    def join(state: DiamondState) -> dict:
        return {"join_result": f"{state['a_result']} + {state['b_result']}"}

    g = StateGraph(DiamondState)
    g.add_node("root", root_node)
    g.add_node("a", a_node)
    g.add_node("b", b_node)
    g.add_node("join", join)
    g.add_edge(START, "root")
    g.add_edge("root", "a")
    g.add_edge("root", "b")
    g.add_edge("a", "join")
    g.add_edge("b", "join")
    g.add_edge("join", END)
    graph = g.compile()

    graph.invoke(DiamondState())

    assert len(registry.registered) == 3  # root, a, b
    root_call, a_call, b_call = registry.registered
    assert a_call["parent_session_id"] == b_call["parent_session_id"]
    assert a_call["parent_token"] == b_call["parent_token"]
    # and it's genuinely the root's session, not some third value
    assert a_call["parent_session_id"] != ""
    assert root_call.get("parent_session_id", "") == ""  # root has no parent


def test_join_node_chains_off_new_parent_not_root():
    """A node built with sets_parent_for_children=True (e.g. senior_approver)
    overwrites governance_parent for its own downstream node, which must NOT reuse
    the original root's proof."""
    registry = FakePGRegistry()

    root_node = governed_node(
        agent_id="root", agent_role="claims_intake", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "t", tools=[], result_key="root_result", sets_parent_for_children=True,
    )
    approver_node = governed_node(
        agent_id="approver", agent_role="senior_approver", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "t", tools=[], result_key="join_result", sets_parent_for_children=True,
    )
    notifier_node = governed_node(
        agent_id="notifier", agent_role="notifier", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "t", tools=[], result_key="a_result",
    )

    class ChainState(TypedDict, total=False):
        governance_parent: GovernanceParent
        root_result: str
        join_result: str
        a_result: str

    g = StateGraph(ChainState)
    g.add_node("root", root_node)
    g.add_node("approver", approver_node)
    g.add_node("notifier", notifier_node)
    g.add_edge(START, "root")
    g.add_edge("root", "approver")
    g.add_edge("approver", "notifier")
    g.add_edge("notifier", END)
    graph = g.compile()
    graph.invoke(ChainState())

    root_call, approver_call, notifier_call = registry.registered
    assert notifier_call["parent_session_id"] != root_call.get("parent_session_id", "")
    # notifier's parent is the session registered for "approver", i.e. registry's 2nd registration
    assert notifier_call["parent_session_id"] == "pg-session-2"


# -- fails closed on mis-wiring ------------------------------------------


def test_concurrent_write_to_parent_key_fails_closed():
    """If two concurrent siblings are (incorrectly) both built with
    sets_parent_for_children=True, LangGraph's own state reducer must reject the
    graph run rather than silently pick one and merge — the safety property is
    structural, not just 'we didn't write this bug'."""
    registry = FakePGRegistry()

    root_node = governed_node(
        agent_id="root", agent_role="claims_intake", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "t", tools=[], result_key="root_result", sets_parent_for_children=True,
    )
    broken_a = governed_node(
        agent_id="a", agent_role="fraud_investigator", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "t", tools=[], result_key="a_result", sets_parent_for_children=True,  # misconfigured
    )
    broken_b = governed_node(
        agent_id="b", agent_role="compliance_reviewer", pg=registry, loop=_end_turn_loop(),
        task=lambda s: "t", tools=[], result_key="b_result", sets_parent_for_children=True,  # misconfigured
    )

    g = StateGraph(DiamondState)
    g.add_node("root", root_node)
    g.add_node("a", broken_a)
    g.add_node("b", broken_b)
    g.add_edge(START, "root")
    g.add_edge("root", "a")
    g.add_edge("root", "b")
    g.add_edge("a", END)
    g.add_edge("b", END)
    graph = g.compile()

    with pytest.raises(InvalidUpdateError):
        graph.invoke(DiamondState())


# -- lifecycle integrity --------------------------------------------------


def test_node_failure_still_evicts_session():
    registry = FakePGRegistry()

    class RaisingAnthropic:
        class _Messages:
            def create(self, **kwargs):
                raise RuntimeError("boom")

        def __init__(self):
            self.messages = self._Messages()

    loop = GovernedAgentLoop(
        anthropic_client=RaisingAnthropic(),
        perchguard_url="http://localhost:8080",
        mcp_url="http://localhost:8090",
        model="claude-haiku-4-5-20251001",
        verbose=False,
    )
    node = governed_node(
        agent_id="a", agent_role="fraud_investigator", pg=registry, loop=loop,
        task=lambda s: "t", tools=[], result_key="a_result",
    )

    with pytest.raises(RuntimeError):
        node({})

    assert len(registry.registered) == 1
    assert registry.evicted == ["pg-session-1"]


# -- client-level guard against unproven delegation -----------------------


def test_registry_rejects_child_registration_without_parent_token():
    """Sanity check on the test double itself: mirrors the real client's
    fail-fast behavior (see test_client.py) so a bug in a node that forgets to
    pass parent_token surfaces here too, not just against a live server."""
    registry = FakePGRegistry()
    with pytest.raises(PerchGuardConfigError):
        registry.register(
            agent_id="a", agent_role="fraud_investigator", intent="t",
            parent_session_id="pg-session-1", parent_token=None,
        )
