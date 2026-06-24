# Agent Integration

Three ways to wire an existing LLM agent to PerchGuard, in order of integration depth.

---

## Option 1 — Direct `/intercept`

The agent calls PerchGuard before every tool execution. Minimal changes to existing agent code.

**Request:**

```
POST /intercept
Content-Type: application/json

{
  "uid":        "unique-request-id",
  "session_id": "agent-session-abc",
  "agent_id":   "my-agent-v1",
  "agent_role": "developer_agent",
  "user_intent": "the user's original request text",
  "tool_call": {
    "name": "bash",
    "parameters": {"command": "git log --oneline -10"}
  }
}
```

**Response:**

```json
{
  "uid":      "unique-request-id",
  "decision": "ALLOW",
  "reason":   "all checks passed"
}
```

| Decision | Meaning | HTTP status |
|----------|---------|-------------|
| `ALLOW` | Execute the tool | 200 |
| `MUTATE` | Execute the mutated call in `mutated_call` | 200 |
| `DENY` | Do not execute | 403 |
| `TERMINATE` | End the session immediately | 403 |
| `HUMAN_REVIEW` | Wait — human approval in progress | 200 (async) |

**Pseudocode pattern:**

```python
def call_tool(session_id, tool_name, params, user_intent):
    resp = requests.post("http://perchguard:8080/intercept", json={
        "uid":        str(uuid4()),
        "session_id": session_id,
        "agent_id":   AGENT_ID,
        "agent_role": AGENT_ROLE,
        "user_intent": user_intent,
        "tool_call":  {"name": tool_name, "parameters": params},
    })
    decision = resp.json()
    if decision["decision"] == "ALLOW":
        return execute_tool(tool_name, params)
    elif decision["decision"] == "MUTATE":
        mutated = decision["mutated_call"]
        return execute_tool(mutated["name"], mutated["parameters"])
    else:
        raise GovernanceError(decision["reason"])
```

**Output validation** — optionally scan what the tool returns:

```
POST /validate/output
{
  "uid": "...",
  "session_id": "...",
  "tool_call": { ... },
  "tool_output": "<raw tool output string>"
}
```

---

## Option 2 — MCP Proxy

PerchGuard acts as a transparent MCP proxy. The agent connects to PerchGuard instead of the real MCP server. No changes to the agent — swap the MCP endpoint URL.

```
Agent → POST http://perchguard:8080/mcp  →  PerchGuard  →  Real MCP Server
```

**Configuration:**

```bash
PERCHGUARD_MCP_UPSTREAM=http://your-mcp-server:8090
PERCHGUARD_ADDR=:8080
```

Start in proxy mode:

```bash
./perchguard --mode=mcp-proxy
```

The agent points its MCP client at PerchGuard's address. PerchGuard intercepts every `tools/call` JSON-RPC request, runs the admission pipeline, and forwards approved calls to the upstream.

**Agent role:** Set `PERCHGUARD_AGENT_ROLE` (default: `developer_agent`) or pass it in the session manifest.

---

## Option 3 — Registered Agent (Declared Intent)

Register the agent's mission before it starts. PerchGuard uses the mission to enrich every call through the semantic firewall — "does this tool call serve the declared mission?"

**Register:**

```
POST /agents/register
Content-Type: application/json

{
  "agent_id": "insurance-claims-agent",
  "manifest": {
    "mission": {
      "summary": "Process insurance claims for the UK motor portfolio",
      "scope": ["read_claim", "search_claims", "write_report"],
      "out_of_scope": ["get_billing_info", "update_medication", "external_api_call"]
    },
    "role": "read_only_agent"
  }
}
```

**Response:**

```json
{
  "session_id": "sess-abc123",
  "agent_token": "pgat-..."
}
```

Include the token on every subsequent `/intercept` call:

```
X-PerchGuard-Agent-Token: pgat-...
```

**Delegation** — sub-agents inherit a capped budget from the parent:

```json
{
  "agent_id":        "claims-summarizer",
  "parent_session_id": "sess-abc123",
  "manifest": { ... }
}
```

The child session gets `delegationFraction` (default 50%) of the parent's remaining tool call budget.

**Delete session** when the agent finishes — triggers governance record emission:

```
DELETE /agents/{session_id}
```

---

## Choosing an option

| Situation | Option |
|-----------|--------|
| Agent you control, any language | Direct `/intercept` |
| Existing MCP-based agent, minimal changes | MCP proxy |
| Regulated use case, semantic alignment checks | Registered agent with manifest |
| All of the above | Register first, then use `/intercept` with token |

The registered agent pattern gives the richest governance — every call is checked for mission alignment, the audit trail includes lineage, and governance snapshots are emitted when the session ends.
