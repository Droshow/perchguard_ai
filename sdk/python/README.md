# perchguard (Python SDK)

Typed client for [PerchGuard](https://github.com/Droshow/perchguard_ai)'s agentic
admission control API. Replaces hand-rolled HTTP clients like the old
`admission_client.py` — one import gets you typed decisions, session lifecycle,
and error handling that doesn't collapse "PerchGuard is down" and "PerchGuard
terminated this session for cause" into the same string.

## Install

```bash
pip install perchguard          # once published
# or, from a checkout / for lab development:
pip install -e sdk/python
```

## Two integration patterns

PerchGuard supports governing an agent two ways. This SDK is for the first one;
the second one needs no SDK at all — don't reach for `GovernedAgentLoop` if you're
already on the proxy.

1. **Explicit** — your agent code calls PerchGuard itself, tool call by tool
   call. Use `PerchGuardClient` directly, or `GovernedAgentLoop` if you also
   want the iterate-until-done loop handled for you.
2. **Transparent (MCP proxy)** — your agent talks to what looks like a normal
   MCP server; PerchGuard's proxy (`--mode=wrap`) intercepts underneath and your
   agent code never mentions PerchGuard. See `deployments/healthcare-agent-python`
   for a live example. Nothing in this SDK is required for that pattern.

## Low-level client

```python
from perchguard import PerchGuardClient, Action

pg = PerchGuardClient(base_url="http://localhost:8080")

with pg.register(
    agent_id="insurance-agent",
    agent_role="developer_agent",
    intent="Process claim #4471 for policy holder...",
    phases=["verify policy", "assess claim", "issue determination"],
    out_of_scope=["payment execution", "policy modification"],
) as session:
    decision = pg.intercept(
        session=session,
        uid=tool_use_block.id,
        tool_name=tool_use_block.name,
        parameters=tool_use_block.input,
    )
    if decision.blocked:  # DENY or TERMINATE
        ...

    out = pg.validate_output(session=session, output=raw_tool_output)
    # session is auto-evicted on `with` exit
```

`user_intent` on `intercept()` defaults to the intent text passed at `register()`
time — override it per-call only if a single session covers more than one
declared task.

### Errors

- `PerchGuardUnavailableError` — transport failure or an unexpected status
  code. PerchGuard's answer is unknown; this is *not* the same as a real
  `TERMINATE` decision, which is returned normally as a `Decision`, not raised.
- `PerchGuardValidationError` — the manifest submitted to `register()` was
  rejected (HTTP 400).
- `PerchGuardAuthError` — `evict()` was called with a missing/invalid API key.
  Evict is the one call that goes through PerchGuard's operator-authenticated
  `/api/*` surface, unlike register/intercept/validate_output, which are
  unauthenticated by design (agent-facing).
- `PerchGuardConfigError` — SDK misuse, e.g. `evict()` with no api_key
  configured anywhere.

## High-level loop helper

Optional sugar over the low-level client for the explicit pattern — collapses
the classic `while iteration < max_iterations` tool-use loop, both governance
calls, and the `messages` splicing into one `.run()`.

```python
from perchguard import GovernedAgentLoop
import anthropic

loop = GovernedAgentLoop(
    anthropic_client=anthropic.Anthropic(),
    perchguard_url="http://localhost:8080",
    mcp_url="http://localhost:8090",
    model="claude-haiku-4-5-20251001",
)

result = loop.run(
    task="Process claim #4471...",
    agent_id="insurance-agent",
    agent_role="developer_agent",
    tools=TOOL_DEFINITIONS,
)
```

`GovernedAgentLoop` still calls out to a caller-configured MCP endpoint
(`mcp_url`) for tool execution — it doesn't own tool execution itself, just the
governance around it. Session lifecycle (register → evict) is automatic unless
you pass an existing `session=` in, in which case you own it (useful across
multiple `run()` calls, or for delegation chains).

## Not in scope

- Not a rewrite of the admission pipeline — every capability here already
  exists server-side; this is packaging, not new governance logic.
- Not a replacement for the MCP-proxy path.
- Sync-only. No `AsyncPerchGuardClient` yet — open an issue if you have a real
  async caller.
