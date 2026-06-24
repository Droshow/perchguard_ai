#!/usr/bin/env python3
"""
PerchGuard Practice Lab — Phase 2: Explicit Governance Agent

Real agentic loop using the Anthropic SDK. Claude calls real tools via the
insurance-mcp-server. PerchGuard intercepts every call before execution and
validates every output after execution. The agent is governance-aware.
"""

import os
import sys
import uuid
import anthropic

from admission_client import PerchGuard
from mcp_client import MCPClient
from tools import TOOL_DEFINITIONS
from tasks import TASKS

PERCHGUARD_URL = os.getenv("PERCHGUARD_URL", "http://localhost:8080")
MCP_URL = os.getenv("MCP_URL", "http://localhost:8090")
MODEL = os.getenv("CLAUDE_MODEL", "claude-haiku-4-5-20251001")


def extract_text(response) -> str:
    for block in response.content:
        if block.type == "text":
            return block.text
    return "(no text output)"


def run_agent(task: str, session_id: str, agent_role: str = "developer_agent") -> str:
    client = anthropic.Anthropic()
    pg = PerchGuard(PERCHGUARD_URL)
    mcp = MCPClient(MCP_URL)

    messages = [{"role": "user", "content": task}]
    iteration = 0
    max_iterations = 10  # safety ceiling

    while iteration < max_iterations:
        iteration += 1
        response = client.messages.create(
            model=MODEL,
            max_tokens=1024,
            tools=TOOL_DEFINITIONS,
            messages=messages,
        )

        if response.stop_reason == "end_turn":
            return extract_text(response)

        tool_results = []
        for block in response.content:
            if block.type != "tool_use":
                continue

            print(f"    [{block.name}] params={block.input}", flush=True)

            # ── INBOUND GOVERNANCE ───────────────────────────────────────
            decision = pg.intercept(
                uid=block.id,
                session_id=session_id,
                agent_id="insurance-agent",
                agent_role=agent_role,
                user_intent=task,
                tool_name=block.name,
                parameters=block.input,
            )
            action_symbol = "✓" if decision.action == "ALLOW" else ("~" if decision.action == "MUTATE" else "✗")
            print(f"    {action_symbol} INBOUND  [{block.name}] → {decision.action}: {decision.reason}", flush=True)

            if decision.action in ("DENY", "TERMINATE"):
                tool_output = f"[GOVERNANCE BLOCKED: {decision.reason}]"
            else:
                params = decision.mutated_params or block.input
                if decision.action == "MUTATE":
                    print(f"      mutated params: {params}", flush=True)

                raw_output = mcp.call_tool(block.name, params)

                # ── OUTBOUND GOVERNANCE ──────────────────────────────────
                out_decision = pg.validate_output(session_id, raw_output)
                out_symbol = "✓" if out_decision.action == "ALLOW" else ("~" if out_decision.action == "MUTATE" else "✗")
                print(f"    {out_symbol} OUTBOUND [{block.name}] → {out_decision.action}: {out_decision.reason}", flush=True)

                if out_decision.action == "DENY":
                    tool_output = "[OUTPUT BLOCKED by governance]"
                elif out_decision.action == "MUTATE" and out_decision.sanitized_output:
                    tool_output = out_decision.sanitized_output
                else:
                    tool_output = raw_output

            tool_results.append({
                "type": "tool_result",
                "tool_use_id": block.id,
                "content": tool_output,
            })

        messages.append({"role": "assistant", "content": response.content})
        messages.append({"role": "user", "content": tool_results})

    return "(agent hit max_iterations safety ceiling)"


def main():
    # Allow running a single task by name: python agent.py poisoned_claim
    task_filter = sys.argv[1] if len(sys.argv) > 1 else None

    print(f"PerchGuard: {PERCHGUARD_URL}")
    print(f"MCP server: {MCP_URL}")
    print(f"Model:      {MODEL}")
    print()

    for task_id, agent_role, task in TASKS:
        if task_filter and task_id != task_filter:
            continue

        session_id = f"pg-lab-{task_id}-{uuid.uuid4().hex[:8]}"
        print("=" * 70)
        print(f"TASK:    {task_id}")
        print(f"ROLE:    {agent_role}")
        print(f"SESSION: {session_id}")
        print(f"INTENT:  {task}")
        print("-" * 70)

        try:
            result = run_agent(task, session_id, agent_role)
        except Exception as e:
            result = f"[ERROR: {e}]"

        print()
        print(f"CLAUDE: {result}")
        print()


if __name__ == "__main__":
    main()
