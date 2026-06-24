import requests


class MCPClient:
    def __init__(self, url: str):
        self.url = url.rstrip("/")
        self._next_id = 1

    def _rpc(self, method: str, params: dict) -> dict:
        req_id = self._next_id
        self._next_id += 1
        resp = requests.post(
            self.url,
            json={"jsonrpc": "2.0", "id": req_id, "method": method, "params": params},
            timeout=10,
        )
        resp.raise_for_status()
        data = resp.json()
        if "error" in data:
            raise RuntimeError(f"MCP error: {data['error']}")
        return data.get("result", {})

    def list_tools(self) -> list[dict]:
        result = self._rpc("tools/list", {})
        return result.get("tools", [])

    def call_tool(self, name: str, arguments: dict) -> str:
        result = self._rpc("tools/call", {"name": name, "arguments": arguments})
        content = result.get("content", [])
        if content:
            return content[0].get("text", "")
        return ""
