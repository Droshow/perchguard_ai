#!/usr/bin/env python3
"""
PerchGuard Healthcare Lab — Phase 3 Validation Agent

Transparent governance: the agent speaks raw MCP JSON-RPC to PerchGuard's /mcp
endpoint. PerchGuard runs the full admission pipeline (quota → inbound validators
→ agent fleet → mutators → outbound scan) and either forwards to the real
healthcare-mcp-server or blocks, entirely invisibly to this code.

No admission_client.py — no explicit /intercept calls. This is the key
architectural difference from the insurance lab. Governance is zero-touch.

Observable outcomes live in PerchGuard's audit log (stdout JSON), not here.
"""

import os
import sys
import uuid
import anthropic

from mcp_client import MCPClient
from tools import TOOL_DEFINITIONS
from tasks import TASKS

# In Docker Compose, PERCHGUARD_MCP_URL points to PerchGuard's /mcp endpoint.
# The agent treats this as if it were a normal MCP server.
MCP_URL = os.getenv("PERCHGUARD_MCP_URL", "http://localhost:8080") + "/mcp"
MODEL = os.getenv("CLAUDE_MODEL", "claude-haiku-4-5-20251001")


def extract_text(response) -> str:
    for block in response.content:
        if block.type == "text":
            return block.text
    return "(no text output)"


def run_agent(task: str, session_id: str, agent_role: str) -> str:
    client = anthropic.Anthropic()
    mcp = MCPClient(MCP_URL, agent_role=agent_role, session_id=session_id, user_intent=task)

    messages = [{"role": "user", "content": task}]
    iteration = 0
    max_iterations = 12  # safety ceiling

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

            # Call the tool via PerchGuard's transparent MCP proxy.
            # Governance (inbound + outbound) happens inside PerchGuard — invisible here.
            try:
                tool_output = mcp.call_tool(block.name, block.input)
                print(f"    ✓ [{block.name}] → {len(tool_output)} chars", flush=True)
            except RuntimeError as e:
                # PerchGuard returns a JSON-RPC error when a tool call is blocked.
                # The agent receives a structured error and can decide how to proceed.
                tool_output = f"[TOOL BLOCKED: {e}]"
                print(f"    ✗ [{block.name}] → {tool_output}", flush=True)

            tool_results.append({
                "type": "tool_result",
                "tool_use_id": block.id,
                "content": tool_output,
            })

        messages.append({"role": "assistant", "content": response.content})
        messages.append({"role": "user", "content": tool_results})

    return "(agent hit max_iterations safety ceiling)"


def main():
    # Allow filtering to a single scenario via CLI arg or TASK env var.
    # docker-compose sets TASK; local runs can use: python agent.py poisoned_patient_note
    task_filter = sys.argv[1] if len(sys.argv) > 1 else os.getenv("TASK")

    print(f"PerchGuard MCP proxy: {MCP_URL}")
    print(f"Model:               {MODEL}")
    print(f"Governance:          transparent (no explicit /intercept calls)")
    print()

    for task_id, agent_role, task in TASKS:
        if task_filter and task_id != task_filter:
            continue

        session_id = f"pg-hc-{task_id}-{uuid.uuid4().hex[:8]}"
        print("=" * 70)
        print(f"SCENARIO: {task_id}")
        print(f"ROLE:     {agent_role}")
        print(f"SESSION:  {session_id}")
        print(f"INTENT:   {task[:80]}...")
        print("-" * 70)
        print("(governance decisions visible in perchguard container audit log)")
        print()

        try:
            result = run_agent(task, session_id, agent_role)
        except Exception as e:
            result = f"[ERROR: {e}]"

        print()
        print(f"CLAUDE: {result}")
        print()


if __name__ == "__main__":
    main()
