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
from perchguard import GovernedAgentLoop

from tools import TOOL_DEFINITIONS
from tasks import TASKS

PERCHGUARD_URL = os.getenv("PERCHGUARD_URL", "http://localhost:8080")
MCP_URL = os.getenv("MCP_URL", "http://localhost:8090")
MODEL = os.getenv("CLAUDE_MODEL", "claude-haiku-4-5-20251001")


def main():
    # Allow running a single task by name: python agent.py poisoned_claim
    task_filter = sys.argv[1] if len(sys.argv) > 1 else None

    print(f"PerchGuard: {PERCHGUARD_URL}")
    print(f"MCP server: {MCP_URL}")
    print(f"Model:      {MODEL}")
    print()

    loop = GovernedAgentLoop(
        anthropic_client=anthropic.Anthropic(),
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
