import requests
from dataclasses import dataclass
from typing import Optional


@dataclass
class Decision:
    action: str          # ALLOW | DENY | MUTATE | TERMINATE | HUMAN_REVIEW
    reason: str
    mutated_params: Optional[dict] = None
    sanitized_output: Optional[str] = None


class PerchGuard:
    def __init__(self, url: str):
        self.url = url.rstrip("/")

    def intercept(
        self,
        uid: str,
        session_id: str,
        agent_id: str,
        agent_role: str,
        user_intent: str,
        tool_name: str,
        parameters: dict,
    ) -> Decision:
        payload = {
            "uid": uid,
            "session_id": session_id,
            "agent_id": agent_id,
            "agent_role": agent_role,
            "user_intent": user_intent,
            "tool_call": {
                "name": tool_name,
                "parameters": parameters,
            },
        }
        try:
            resp = requests.post(f"{self.url}/intercept", json=payload, timeout=10)
            resp.raise_for_status()
        except requests.HTTPError as e:
            # 403 = session terminated by PerchGuard after risk threshold crossed
            return Decision(
                action="TERMINATE",
                reason=f"session terminated by PerchGuard (HTTP {e.response.status_code})",
            )
        data = resp.json()

        mutated_params = None
        if data.get("mutated_call") and data["mutated_call"].get("parameters"):
            mutated_params = data["mutated_call"]["parameters"]

        return Decision(
            action=data.get("decision", "DENY"),
            reason=data.get("reason", ""),
            mutated_params=mutated_params,
        )

    def validate_output(self, session_id: str, output: str) -> Decision:
        payload = {
            "session_id": session_id,
            "tool_output": output,   # must match ToolCallAdmissionRequest.ToolOutput json tag
        }
        try:
            resp = requests.post(f"{self.url}/validate/output", json=payload, timeout=10)
            resp.raise_for_status()
        except requests.HTTPError as e:
            return Decision(
                action="TERMINATE",
                reason=f"session terminated by PerchGuard (HTTP {e.response.status_code})",
            )
        data = resp.json()

        sanitized = data.get("sanitized_output") or data.get("output")
        return Decision(
            action=data.get("decision", "ALLOW"),
            reason=data.get("reason", ""),
            sanitized_output=sanitized,
        )
