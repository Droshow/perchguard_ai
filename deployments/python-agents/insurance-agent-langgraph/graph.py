"""The claim-review StateGraph (Phase 11): a real fan-out/fan-in delegation graph,
not a straight chain — see sdk/python/perchguard/langgraph_adapter.py's module
docstring for why that shape matters for the parent-verification trust boundary.

    START -> intake -> {investigator, compliance} -> approver -> notifier -> END

`intake` and `approver` are the only nodes built with sets_parent_for_children=True
— they're the only points where a downstream node needs to chain delegation off
them. `investigator` and `compliance` run concurrently and only ever *read*
governance_parent (both get intake's), never write it — see langgraph_adapter's
module docstring for what breaks if that invariant is violated.
"""

from __future__ import annotations

import os
from typing import Any, TypedDict

from langgraph.graph import END, START, StateGraph
from perchguard import GovernedAgentLoop, PerchGuardClient
from perchguard.langgraph_adapter import GovernanceParent, governed_node

from tasks import (
    APPROVAL_TASK_TEMPLATE,
    COMPLIANCE_TASK_TEMPLATE,
    INTAKE_TASK,
    INVESTIGATOR_TASK_TEMPLATE,
    NOTIFY_TASK_TEMPLATE,
)
from tools import (
    APPROVER_TOOL_DEFINITIONS,
    COMPLIANCE_TOOL_DEFINITIONS,
    INTAKE_TOOL_DEFINITIONS,
    INVESTIGATOR_TOOL_DEFINITIONS,
    NOTIFIER_TOOL_DEFINITIONS,
)

PERCHGUARD_URL = os.getenv("PERCHGUARD_URL", "http://localhost:8080")
MCP_URL = os.getenv("MCP_URL", "http://localhost:8090")
MODEL = os.getenv("CLAUDE_MODEL", "claude-haiku-4-5-20251001")


class ClaimReviewState(TypedDict, total=False):
    governance_parent: GovernanceParent
    intake_summary: str
    investigator_findings: str
    compliance_findings: str
    approval_decision: str
    notification_result: str


def _loop(anthropic_client: Any) -> GovernedAgentLoop:
    # A distinct loop instance per node — concurrent nodes (investigator, compliance)
    # never share one, so there's no question of GovernedAgentLoop being called
    # concurrently on the same instance from two threads.
    return GovernedAgentLoop(
        anthropic_client=anthropic_client,
        perchguard_url=PERCHGUARD_URL,
        mcp_url=MCP_URL,
        model=MODEL,
    )


def build_graph(anthropic_client: Any):
    pg = PerchGuardClient(PERCHGUARD_URL)

    intake = governed_node(
        agent_id="insurance-agent-intake",
        agent_role="claims_intake",
        pg=pg,
        loop=_loop(anthropic_client),
        task=lambda _s: INTAKE_TASK,
        tools=INTAKE_TOOL_DEFINITIONS,
        result_key="intake_summary",
        sets_parent_for_children=True,
    )
    investigator = governed_node(
        agent_id="insurance-agent-investigator",
        agent_role="fraud_investigator",
        pg=pg,
        loop=_loop(anthropic_client),
        task=lambda s: INVESTIGATOR_TASK_TEMPLATE.format(intake_summary=s["intake_summary"]),
        tools=INVESTIGATOR_TOOL_DEFINITIONS,
        result_key="investigator_findings",
    )
    compliance = governed_node(
        agent_id="insurance-agent-compliance",
        agent_role="compliance_reviewer",
        pg=pg,
        loop=_loop(anthropic_client),
        task=lambda s: COMPLIANCE_TASK_TEMPLATE.format(intake_summary=s["intake_summary"]),
        tools=COMPLIANCE_TOOL_DEFINITIONS,
        result_key="compliance_findings",
    )
    approver = governed_node(
        agent_id="insurance-agent-approver",
        agent_role="senior_approver",
        pg=pg,
        loop=_loop(anthropic_client),
        task=lambda s: APPROVAL_TASK_TEMPLATE.format(
            investigator_findings=s["investigator_findings"],
            compliance_findings=s["compliance_findings"],
        ),
        tools=APPROVER_TOOL_DEFINITIONS,
        result_key="approval_decision",
        sets_parent_for_children=True,
    )
    notifier = governed_node(
        agent_id="insurance-agent-notifier",
        agent_role="notifier",
        pg=pg,
        loop=_loop(anthropic_client),
        task=lambda s: NOTIFY_TASK_TEMPLATE.format(decision=s["approval_decision"]),
        tools=NOTIFIER_TOOL_DEFINITIONS,
        result_key="notification_result",
    )

    g = StateGraph(ClaimReviewState)
    g.add_node("intake", intake)
    g.add_node("investigator", investigator)
    g.add_node("compliance", compliance)
    g.add_node("approver", approver)
    g.add_node("notifier", notifier)
    g.add_edge(START, "intake")
    g.add_edge("intake", "investigator")
    g.add_edge("intake", "compliance")
    g.add_edge("investigator", "approver")
    g.add_edge("compliance", "approver")
    g.add_edge("approver", "notifier")
    g.add_edge("notifier", END)
    # No checkpointer — see langgraph_adapter.py's module docstring for why.
    return g.compile()
