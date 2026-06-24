import requests


class MCPClient:
    """Thin JSON-RPC 2.0 client for an HTTP MCP server.

    In the healthcare lab this points to PerchGuard's /mcp endpoint, not the
    real MCP server directly. PerchGuard intercepts every call transparently —
    the agent has no knowledge of governance.

    agent_role is sent via X-Perchguard-Role header, allowing PerchGuard to
    apply the correct authorization policy per request without restarting.
    """

    def __init__(self, url: str, agent_role: str = "developer_agent", session_id: str = "", user_intent: str = ""):
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
            # HTTP headers must be ASCII (latin-1). Encode to ASCII, replacing any
            # non-ASCII chars (e.g. em-dashes in task descriptions) with '?'.
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

    def list_tools(self) -> list[dict]:
        result = self._rpc("tools/list", {})
        return result.get("tools", [])

    def call_tool(self, name: str, arguments: dict) -> str:
        """Call a tool. Returns the text content of the first content block."""
        result = self._rpc("tools/call", {"name": name, "arguments": arguments})
        content = result.get("content", [])
        if content:
            return content[0].get("text", "")
        return ""
