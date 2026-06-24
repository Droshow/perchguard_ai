import requests


class MCPClient:
    """Thin JSON-RPC 2.0 client pointing at PerchGuard's /mcp endpoint.

    The agent treats this as a normal MCP server. PerchGuard intercepts every
    tools/call transparently — inbound admission, agent fleet drift scoring,
    parameter mutation, and outbound output scan all run without the agent's knowledge.
    """

    def __init__(self, url: str, agent_role: str = "read_only_agent", session_id: str = "", user_intent: str = ""):
        self.url = url.rstrip("/")
        self.agent_role = agent_role
        self.session_id = session_id
        self.user_intent = user_intent
        self._next_id = 1

    def _rpc(self, method: str, params: dict) -> dict:
        req_id = self._next_id
        self._next_id += 1
        headers = {"X-Perchguard-Role": self.agent_role}
        if self.session_id:
            headers["X-Perchguard-Session-ID"] = self.session_id
        if self.user_intent:
            safe_intent = self.user_intent[:256].encode("ascii", errors="replace").decode("ascii")
            headers["X-Perchguard-User-Intent"] = safe_intent
        resp = requests.post(
            self.url,
            json={"jsonrpc": "2.0", "id": req_id, "method": method, "params": params},
            headers=headers,
            timeout=30,
        )
        resp.raise_for_status()
        data = resp.json()
        if "error" in data and data["error"] is not None:
            raise RuntimeError(f"MCP error {data['error'].get('code')}: {data['error'].get('message')}")
        return data.get("result", {})

    def call_tool(self, name: str, arguments: dict) -> str:
        result = self._rpc("tools/call", {"name": name, "arguments": arguments})
        content = result.get("content", [])
        if content:
            return content[0].get("text", "")
        return ""
