#!/usr/bin/env python3
"""
PerchGuard Practice Lab — Phase 2: Explicit Governance Agent

Real agentic loop using the Anthropic SDK. Claude calls real tools via the
insurance-mcp-server. Every call is governed by PerchGuard's Python SDK
(GovernedAgentLoop) — this file is a task-list driver and owns no governance
logic of its own.
"""

import os
import sys

import anthropic
from perchguard import GovernedAgentLoop, PerchGuardClient

from tools import TOOL_DEFINITIONS, INTAKE_TOOL_DEFINITIONS, INVESTIGATOR_TOOL_DEFINITIONS
from tasks import TASKS, FRAUD_ESCALATION_TASK, INVESTIGATOR_TASK_TEMPLATE

PERCHGUARD_URL = os.getenv("PERCHGUARD_URL", "http://localhost:8080")
MCP_URL = os.getenv("MCP_URL", "http://localhost:8090")
MODEL = os.getenv("CLAUDE_MODEL", "claude-haiku-4-5-20251001")


def run_fraud_escalation(anthropic_client):
    """Claims-intake agent escalates a suspicious claim to a fraud-investigator
    sub-agent via a real tool call — agent-as-tool delegation, not a scripted
    second task. Both sessions are governed; the child is linked to the parent
    via parent_session_id."""
    pg = PerchGuardClient(PERCHGUARD_URL)
    loop = GovernedAgentLoop(
        anthropic_client=anthropic_client,
        perchguard_url=PERCHGUARD_URL,
        mcp_url=MCP_URL,
        model=MODEL,
    )

    def escalate_to_investigator(params):
        claim_id = params["claim_id"]
        reason = params["reason"]
        child_session = pg.register(
            agent_id="insurance-agent-investigator",
            agent_role="fraud_investigator",
            intent=f"Investigate escalated claim {claim_id}",
            parent_session_id=parent_session.id,
        )
        print(f"    SESSION: {child_session.id} (sub-agent of {parent_session.id})", flush=True)
        try:
            return loop.run(
                task=INVESTIGATOR_TASK_TEMPLATE.format(claim_id=claim_id, reason=reason),
                agent_id="insurance-agent-investigator",
                agent_role="fraud_investigator",
                tools=INVESTIGATOR_TOOL_DEFINITIONS,
                session=child_session,
            )
        finally:
            pg.evict(child_session, best_effort=True)

    loop.local_tools = {"escalate_to_investigator": escalate_to_investigator}

    print("=" * 70)
    print("TASK:    fraud_escalation")
    print("ROLE:    claims_intake")
    print(f"INTENT:  {FRAUD_ESCALATION_TASK}")
    print("-" * 70)

    parent_session = pg.register(
        agent_id="insurance-agent",
        agent_role="claims_intake",
        intent=FRAUD_ESCALATION_TASK,
    )
    print(f"    SESSION: {parent_session.id}", flush=True)
    try:
        result = loop.run(
            task=FRAUD_ESCALATION_TASK,
            agent_id="insurance-agent",
            agent_role="claims_intake",
            tools=INTAKE_TOOL_DEFINITIONS,
            session=parent_session,
        )
    except Exception as e:
        result = f"[ERROR: {e}]"
    finally:
        pg.evict(parent_session, best_effort=True)

    print()
    print(f"CLAUDE: {result}")
    print()


def main():
    # Allow running a single task by name: python agent.py poisoned_claim
    task_filter = sys.argv[1] if len(sys.argv) > 1 else None

    print(f"PerchGuard: {PERCHGUARD_URL}")
    print(f"MCP server: {MCP_URL}")
    print(f"Model:      {MODEL}")
    print()

    anthropic_client = anthropic.Anthropic()

    if task_filter in (None, "fraud_escalation"):
        run_fraud_escalation(anthropic_client)

    loop = GovernedAgentLoop(
        anthropic_client=anthropic_client,
        perchguard_url=PERCHGUARD_URL,
        mcp_url=MCP_URL,
        model=MODEL,
    )

    for task_id, agent_role, task in TASKS:
        if task_filter and task_id != task_filter:
            continue

        print("=" * 70)
        print(f"TASK:    {task_id}")
        print(f"ROLE:    {agent_role}")
        print(f"INTENT:  {task}")
        print("-" * 70)

        try:
            result = loop.run(
                task=task,
                agent_id="insurance-agent",
                agent_role=agent_role,
                tools=TOOL_DEFINITIONS,
            )
        except Exception as e:
            result = f"[ERROR: {e}]"

        print()
        print(f"CLAUDE: {result}")
        print()


if __name__ == "__main__":
    main()
