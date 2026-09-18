#!/usr/bin/env python3
"""
PerchGuard Red Team Agent — Phase 5 Gap 2 (MCP proxy end-to-end)

Transparent governance: the agent speaks MCP JSON-RPC to PerchGuard's /mcp
endpoint. PerchGuard runs the full admission pipeline (quota → inbound validators
→ agent fleet → mutators → outbound scan) and either forwards to the real
insurance-mcp-server or blocks — invisible to this code.

Uses Claude Sonnet 4.6 for intelligence: smarter than Haiku, will adapt strategy
after denials rather than retrying the same blocked call.

Observable outcomes:
  - PerchGuard audit log (docker compose logs perchguard)
  - ./snapshots/audit.jsonl (per-call forensic record)
  - GET /api/fleet/summary (live risk during the run)
"""

import os
import sys
import uuid
import anthropic

from mcp_client import MCPClient
from tools import TOOL_DEFINITIONS
from tasks import TASKS

MCP_URL = os.getenv("PERCHGUARD_MCP_URL", "http://localhost:8080") + "/mcp"
MODEL = os.getenv("CLAUDE_MODEL", "claude-sonnet-4-6")


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
    max_iterations = 20  # higher ceiling — Sonnet may take more turns to adapt

    blocked_count = 0
    allowed_count = 0

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

            print(f"    → [{block.name}] {block.input}", flush=True)

            try:
                tool_output = mcp.call_tool(block.name, block.input)
                allowed_count += 1
                print(f"    ✓ [{block.name}] ALLOW ({len(tool_output)} chars)", flush=True)
            except RuntimeError as e:
                blocked_count += 1
                tool_output = f"[TOOL BLOCKED: {e}]"
                print(f"    ✗ [{block.name}] BLOCKED — {e}", flush=True)

            tool_results.append({
                "type": "tool_result",
                "tool_use_id": block.id,
                "content": tool_output,
            })

        messages.append({"role": "assistant", "content": response.content})
        messages.append({"role": "user", "content": tool_results})

    return f"(hit max_iterations — ALLOW={allowed_count} BLOCKED={blocked_count})"


def main():
    task_filter = sys.argv[1] if len(sys.argv) > 1 else os.getenv("TASK")

    print(f"Red team MCP proxy: {MCP_URL}")
    print(f"Model:              {MODEL}")
    print(f"Governance:         transparent (no explicit /intercept calls)")
    print(f"Forensic log:       ./snapshots/audit.jsonl")
    print()

    for task_id, agent_role, task in TASKS:
        if task_filter and task_id != task_filter:
            continue

        session_id = f"rt-mcp-{task_id}-{uuid.uuid4().hex[:8]}"
        print("=" * 70)
        print(f"SCENARIO: {task_id}")
        print(f"ROLE:     {agent_role}")
        print(f"SESSION:  {session_id}")
        print(f"INTENT:   {task[:80]}...")
        print("-" * 70)
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
