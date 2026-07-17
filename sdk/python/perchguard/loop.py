"""High-level loop helper for the explicit-governance integration pattern.

GovernedAgentLoop.run() is the body of insurance-agent-python's old run_agent():
the iterate-until-end_turn loop, both governance calls (inbound intercept,
outbound validate_output), and the messages splicing — collapsed into the SDK
so a caller's agent.py stops owning any governance logic at all.

This is optional sugar over PerchGuardClient. The transparent/proxy pattern
(healthcare-agent-python's --mode=wrap) needs none of this by design — see
README.md.
"""

from __future__ import annotations

from typing import Any, Optional

from .client import PerchGuardClient
from .mcp import MCPClient
from .types import Action, Session

_ACTION_SYMBOL = {
    Action.ALLOW: "✓",
    Action.MUTATE: "~",
}
_DEFAULT_SYMBOL = "✗"


def _symbol(action: Action) -> str:
    return _ACTION_SYMBOL.get(action, _DEFAULT_SYMBOL)


def _extract_text(response: Any) -> str:
    for block in response.content:
        if block.type == "text":
            return block.text
    return "(no text output)"


class GovernedAgentLoop:
    """Runs an Anthropic tool-use loop with every call governed by PerchGuard."""

    def __init__(
        self,
        *,
        anthropic_client: Any,
        perchguard_url: str,
        mcp_url: str,
        model: str,
        perchguard_api_key: Optional[str] = None,
        max_iterations: int = 10,
        verbose: bool = True,
    ) -> None:
        self.anthropic = anthropic_client
        self.pg = PerchGuardClient(perchguard_url, api_key=perchguard_api_key)
        self.mcp = MCPClient(mcp_url)
        self.model = model
        self.max_iterations = max_iterations
        self.verbose = verbose

    def run(
        self,
        *,
        task: str,
        agent_id: str,
        agent_role: str,
        tools: list[dict],
        session: Optional[Session] = None,
        phases: Optional[list[str]] = None,
        out_of_scope: Optional[list[str]] = None,
        max_tokens: int = 1024,
    ) -> str:
        """Run one task to completion (or until max_iterations). Returns the
        final text response.

        If `session` is not supplied, a session is registered from (agent_id,
        agent_role, task) and evicted (best-effort) when this call returns —
        session lifecycle is a first-class concern, not left to the caller.
        Pass an existing session to manage lifecycle yourself (e.g. across
        multiple run() calls, or for delegation chains).
        """
        owns_session = session is None
        if session is None:
            session = self.pg.register(
                agent_id=agent_id,
                agent_role=agent_role,
                intent=task,
                phases=phases,
                out_of_scope=out_of_scope,
            )
            if self.verbose:
                print(f"    SESSION: {session.id}", flush=True)
        try:
            return self._run_loop(task, session, tools, max_tokens)
        finally:
            if owns_session:
                self.pg.evict(session, best_effort=True)

    def _run_loop(self, task: str, session: Session, tools: list[dict], max_tokens: int) -> str:
        messages: list[dict] = [{"role": "user", "content": task}]
        iteration = 0

        while iteration < self.max_iterations:
            iteration += 1
            response = self.anthropic.messages.create(
                model=self.model,
                max_tokens=max_tokens,
                tools=tools,
                messages=messages,
            )

            if response.stop_reason == "end_turn":
                return _extract_text(response)

            tool_results = []
            for block in response.content:
                if block.type != "tool_use":
                    continue

                if self.verbose:
                    print(f"    [{block.name}] params={block.input}", flush=True)

                decision = self.pg.intercept(
                    session=session,
                    uid=block.id,
                    tool_name=block.name,
                    parameters=block.input,
                )
                if self.verbose:
                    print(
                        f"    {_symbol(decision.action)} INBOUND  [{block.name}] -> "
                        f"{decision.action.value}: {decision.reason}",
                        flush=True,
                    )

                if decision.blocked:
                    tool_output = f"[GOVERNANCE BLOCKED: {decision.reason}]"
                else:
                    params = decision.mutated_params or block.input
                    if decision.action == Action.MUTATE and self.verbose:
                        print(f"      mutated params: {params}", flush=True)

                    raw_output = self.mcp.call_tool(block.name, params)

                    out_decision = self.pg.validate_output(session=session, output=raw_output)
                    if self.verbose:
                        print(
                            f"    {_symbol(out_decision.action)} OUTBOUND [{block.name}] -> "
                            f"{out_decision.action.value}: {out_decision.reason}",
                            flush=True,
                        )

                    if out_decision.action == Action.DENY:
                        tool_output = "[OUTPUT BLOCKED by governance]"
                    elif out_decision.action == Action.MUTATE and out_decision.sanitized_output:
                        tool_output = out_decision.sanitized_output
                    else:
                        tool_output = raw_output

                tool_results.append(
                    {"type": "tool_result", "tool_use_id": block.id, "content": tool_output}
                )

            messages.append({"role": "assistant", "content": response.content})
            messages.append({"role": "user", "content": tool_results})

        return "(agent hit max_iterations safety ceiling)"
