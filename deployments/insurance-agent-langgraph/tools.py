"""Tool schemas for the LangGraph fraud-escalation graph (Phase 11).

Same five tools deployments/insurance-mcp-server/main.go already implements
(read_policy, search_claims, write_report, run_sql, send_notification) — no MCP
server changes needed. Each role gets the subset it actually uses in its own
Claude tool-use loop; configs/policies.yaml's allowedTools ceilings (checked by
ValidateScope at registration) are separately what lets authority flow down the
delegation graph and are intentionally broader in places (see policies.yaml's
comment on claims_intake/send_notification).
"""

TOOL_DEFINITIONS = [
    {
        "name": "read_policy",
        "description": "Returns the full text of an insurance policy document by policy ID",
        "input_schema": {
            "type": "object",
            "properties": {
                "policy_id": {"type": "string", "description": "Policy identifier, e.g. POL-2024-0012"},
            },
            "required": ["policy_id"],
        },
    },
    {
        "name": "search_claims",
        "description": "Searches claims by keyword or claimant name, returns matching summaries",
        "input_schema": {
            "type": "object",
            "properties": {
                "query": {"type": "string", "description": "Search terms to match against claims"},
            },
            "required": ["query"],
        },
    },
    {
        "name": "write_report",
        "description": "Writes a text report to /workspace/ on the server",
        "input_schema": {
            "type": "object",
            "properties": {
                "filename": {"type": "string", "description": "Filename under /workspace/"},
                "content": {"type": "string", "description": "Report content"},
            },
            "required": ["filename", "content"],
        },
    },
    {
        "name": "run_sql",
        "description": "Executes a read-only SQL query against the insurance database",
        "input_schema": {
            "type": "object",
            "properties": {
                "query": {"type": "string", "description": "SQL SELECT statement"},
            },
            "required": ["query"],
        },
    },
    {
        "name": "send_notification",
        "description": "Sends a notification message to an approved internal destination",
        "input_schema": {
            "type": "object",
            "properties": {
                "destination": {"type": "string", "description": "Email or internal channel"},
                "message": {"type": "string", "description": "Notification body"},
            },
            "required": ["destination", "message"],
        },
    },
]

_BY_NAME = {t["name"]: t for t in TOOL_DEFINITIONS}

INTAKE_TOOL_DEFINITIONS = [
    _BY_NAME["read_policy"],
    _BY_NAME["search_claims"],
]

INVESTIGATOR_TOOL_DEFINITIONS = [
    _BY_NAME["search_claims"],
    _BY_NAME["run_sql"],
    _BY_NAME["write_report"],
]

COMPLIANCE_TOOL_DEFINITIONS = [
    _BY_NAME["read_policy"],
    _BY_NAME["search_claims"],
    _BY_NAME["write_report"],
]

APPROVER_TOOL_DEFINITIONS = [
    _BY_NAME["write_report"],
]

NOTIFIER_TOOL_DEFINITIONS = [
    _BY_NAME["send_notification"],
]
