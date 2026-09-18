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

ESCALATE_TOOL = {
    "name": "escalate_to_investigator",
    "description": "Hands off a suspicious claim to a fraud-investigator sub-agent for deeper review",
    "input_schema": {
        "type": "object",
        "properties": {
            "claim_id": {"type": "string", "description": "Claim identifier, e.g. CLM-9985"},
            "reason": {"type": "string", "description": "Why this claim looks suspicious"},
        },
        "required": ["claim_id", "reason"],
    },
}

_BY_NAME = {t["name"]: t for t in TOOL_DEFINITIONS}

INTAKE_TOOL_DEFINITIONS = [
    _BY_NAME["read_policy"],
    _BY_NAME["search_claims"],
    ESCALATE_TOOL,
]

INVESTIGATOR_TOOL_DEFINITIONS = [
    _BY_NAME["search_claims"],
    _BY_NAME["run_sql"],
    _BY_NAME["write_report"],
]
