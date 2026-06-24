#!/usr/bin/env python3
"""Minimal mock MCP upstream server for PerchGuard demo.

Responds to any JSON-RPC tools/call with a plausible text result,
and to initialize/tools/list so Copilot-style clients can discover tools.

Run:  python3 scripts/mock-mcp-upstream.py
Listens on :3000
"""

import json
import http.server
import time

TOOLS = [
    {"name": "read_file",   "description": "Read a file", "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}}}},
    {"name": "write_file",  "description": "Write a file", "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}, "content": {"type": "string"}}}},
    {"name": "list_dir",    "description": "List directory", "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}}}},
    {"name": "run_command", "description": "Run a shell command", "inputSchema": {"type": "object", "properties": {"command": {"type": "string"}}}},
    {"name": "search_files","description": "Search files", "inputSchema": {"type": "object", "properties": {"query": {"type": "string"}}}},
]

FAKE_RESULTS = {
    "read_file":    "# Calorie Tracker\nA simple app to track daily calorie intake.\n",
    "write_file":   "File written successfully.",
    "list_dir":     "main.go  go.mod  README.md  handlers/  models/  storage/",
    "run_command":  "ok  github.com/user/calorie-tracker  0.005s",
    "search_files": "Found 3 matches in handlers/food.go, models/meal.go, storage/db.go",
}

class MCPHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        print(f"[mock-mcp] {fmt % args}")

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        try:
            req = json.loads(body)
        except Exception:
            self._respond({"jsonrpc": "2.0", "id": None, "error": {"code": -32700, "message": "parse error"}})
            return

        method = req.get("method", "")
        rid = req.get("id")

        if method == "initialize":
            result = {
                "protocolVersion": "2024-11-05",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "mock-mcp-upstream", "version": "1.0.0"},
            }
        elif method == "tools/list":
            result = {"tools": TOOLS}
        elif method == "tools/call":
            tool_name = req.get("params", {}).get("name", "unknown")
            text = FAKE_RESULTS.get(tool_name, f"[mock] {tool_name} executed successfully")
            result = {"content": [{"type": "text", "text": text}]}
        elif method == "notifications/initialized":
            # Notification — no response needed
            self.send_response(204)
            self.end_headers()
            return
        else:
            result = {"content": [{"type": "text", "text": f"[mock] method={method} handled"}]}

        self._respond({"jsonrpc": "2.0", "id": rid, "result": result})

    def _respond(self, payload):
        data = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


if __name__ == "__main__":
    server = http.server.HTTPServer(("", 3000), MCPHandler)
    print("[mock-mcp] listening on :3000")
    server.serve_forever()
