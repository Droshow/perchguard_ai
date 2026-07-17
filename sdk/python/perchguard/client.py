"""Low-level typed transport for PerchGuard's admission API.

Wraps POST /agents/register, POST /intercept, POST /validate/output, and
DELETE /api/sessions/{id} — the exact contracts in pkg/api/agents.go,
pkg/admission/interceptor.go, and pkg/api/sessions.go. See PHASE9B-PYTHON-SDK.md
for the design rationale.
"""

from __future__ import annotations

import datetime
import warnings
from typing import Any, Optional

import requests

from .exceptions import (
    PerchGuardAuthError,
    PerchGuardConfigError,
    PerchGuardUnavailableError,
    PerchGuardValidationError,
)
from .types import Action, Decision, Session

# HTTP statuses on /intercept and /validate/output that carry a real decision,
# not a transport failure. 403 is how the server signals DENY/TERMINATE and
# invalid-token rejections (see interceptor.go ServeHTTP) — it must NOT be
# collapsed into "PerchGuard is down", which is exactly the bug this SDK fixes.
_DECISION_STATUSES = frozenset({200, 403})


class PerchGuardClient:
    """Typed client for PerchGuard's agent-facing admission endpoints.

    `register`, `intercept`, and `validate_output` talk to unauthenticated
    agent endpoints. `evict` talks to the operator-authenticated
    `/api/sessions/{id}` endpoint and requires an api_key (from here or from
    the call itself) — this asymmetry is intentional, see CLAUDE.md.
    """

    def __init__(
        self,
        base_url: str,
        *,
        api_key: Optional[str] = None,
        timeout: float = 10.0,
        http: Any = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout
        self._http = http or requests

    # -- session lifecycle -------------------------------------------------

    def register(
        self,
        *,
        agent_id: str,
        agent_role: str,
        intent: str,
        phases: Optional[list[str]] = None,
        out_of_scope: Optional[list[str]] = None,
        scope: Optional[list[str]] = None,
        owner: str = "unknown",
        version: str = "0.1.0",
        manifest_id: Optional[str] = None,
        parent_session_id: str = "",
    ) -> Session:
        """Register a session, pre-seeding its intent baseline before any tool call.

        Builds an AgentManifest (pkg/manifest/manifest.go) from these kwargs so
        callers don't need to hand-write manifest YAML/JSON for the common case.
        """
        manifest = {
            "apiVersion": "perchguard.ai/v1",
            "kind": "AgentManifest",
            "metadata": {
                "id": manifest_id or agent_id,
                "owner": owner,
                "created": datetime.date.today().isoformat(),
                "version": version,
            },
            "mission": {
                "summary": intent,
                "scope": scope or [],
                "out_of_scope": out_of_scope or [],
                "phases": phases or [],
            },
            "authorization": {
                "role": agent_role,
                "allowed_systems": [],
                "human_review_required_for": [],
            },
            "invariants": [],
            "project_context": {},
            "parent_session_id": parent_session_id,
        }

        try:
            resp = self._http.post(
                f"{self.base_url}/agents/register", json=manifest, timeout=self.timeout
            )
        except requests.RequestException as e:
            raise PerchGuardUnavailableError(f"could not reach PerchGuard: {e}") from e

        if resp.status_code == 400:
            raise PerchGuardValidationError(_error_message(resp))
        if resp.status_code != 201:
            raise PerchGuardUnavailableError(
                f"unexpected response registering session (HTTP {resp.status_code}): {_error_message(resp)}"
            )

        data = resp.json()
        return Session(
            id=data["session_id"],
            agent_id=data["agent_id"],
            agent_role=agent_role,
            manifest_version=data["manifest_version"],
            token=data["token"],
            intent=intent,
            effective_policies=data.get("effective_policies") or [],
            context_loaded=data.get("context_loaded", False),
            parent_session_id=data.get("parent_session_id", ""),
            _client=self,
        )

    def evict(
        self,
        session: "Session | str",
        *,
        api_key: Optional[str] = None,
        best_effort: bool = False,
    ) -> None:
        """Evict a session (DELETE /api/sessions/{id}). Requires an API key.

        Unlike register/intercept/validate_output, this hits an operator-
        authenticated route — pass api_key here or set one on the client.
        With best_effort=True (used by Session's context-manager exit),
        failures are swallowed as warnings instead of raised, so cleanup
        never masks the exception that triggered it.
        """
        session_id = session.id if isinstance(session, Session) else session
        key = api_key or self.api_key
        if not key:
            if best_effort:
                warnings.warn(
                    "PerchGuard session left un-evicted: no api_key configured for evict()",
                    stacklevel=2,
                )
                return
            raise PerchGuardConfigError(
                "evict() requires an api_key — pass it to PerchGuardClient(...) or evict(api_key=...)"
            )

        try:
            resp = self._http.delete(
                f"{self.base_url}/api/sessions/{session_id}",
                headers={"Authorization": f"Bearer {key}"},
                timeout=self.timeout,
            )
        except requests.RequestException as e:
            if best_effort:
                warnings.warn(f"PerchGuard evict failed: {e}", stacklevel=2)
                return
            raise PerchGuardUnavailableError(f"could not reach PerchGuard: {e}") from e

        if resp.status_code in (401, 403):
            if best_effort:
                warnings.warn(f"PerchGuard evict rejected: {_error_message(resp)}", stacklevel=2)
                return
            raise PerchGuardAuthError(_error_message(resp))
        if resp.status_code not in (200, 404):
            if best_effort:
                warnings.warn(
                    f"PerchGuard evict returned HTTP {resp.status_code}: {_error_message(resp)}",
                    stacklevel=2,
                )
                return
            raise PerchGuardUnavailableError(
                f"unexpected response evicting session (HTTP {resp.status_code}): {_error_message(resp)}"
            )

    # -- per-call governance -------------------------------------------------

    def intercept(
        self,
        *,
        session: Session,
        uid: str,
        tool_name: str,
        parameters: dict[str, Any],
        user_intent: Optional[str] = None,
        conversation: Optional[list[dict[str, str]]] = None,
        source: str = "",
        destination_url: str = "",
        nesting_depth: int = 0,
        data_refs_in: Optional[list[str]] = None,
        metadata: Optional[dict[str, str]] = None,
    ) -> Decision:
        """POST /intercept — inbound admission for one proposed tool call."""
        payload = {
            "uid": uid,
            "session_id": session.id,
            "agent_id": session.agent_id,
            "agent_role": session.agent_role,
            "user_intent": user_intent if user_intent is not None else session.intent,
            "conversation": conversation or [],
            "tool_call": {
                "name": tool_name,
                "parameters": parameters,
                "source": source,
                "destination_url": destination_url,
            },
            "nesting_depth": nesting_depth,
            "data_refs_in": data_refs_in or [],
            "metadata": metadata or {},
        }
        return self._post_governance("/intercept", payload, session)

    def validate_output(
        self,
        *,
        session: Session,
        output: str,
        uid: str = "",
    ) -> Decision:
        """POST /validate/output — outbound admission for a tool result."""
        payload = {
            "uid": uid,
            "session_id": session.id,
            "agent_id": session.agent_id,
            "tool_output": output,
        }
        return self._post_governance("/validate/output", payload, session)

    # -- internals -------------------------------------------------------

    def _post_governance(self, path: str, payload: dict[str, Any], session: Session) -> Decision:
        headers = {"X-PerchGuard-Agent-Token": session.token} if session.token else {}
        try:
            resp = self._http.post(
                f"{self.base_url}{path}", json=payload, headers=headers, timeout=self.timeout
            )
        except requests.RequestException as e:
            raise PerchGuardUnavailableError(f"could not reach PerchGuard: {e}") from e

        if resp.status_code not in _DECISION_STATUSES:
            raise PerchGuardUnavailableError(
                f"unexpected response from {path} (HTTP {resp.status_code}): {_error_message(resp)}"
            )

        data = resp.json()
        mutated_params = None
        mutated_call = data.get("mutated_call")
        if mutated_call and mutated_call.get("parameters"):
            mutated_params = mutated_call["parameters"]

        return Decision(
            action=Action(data.get("decision", "DENY")),
            reason=data.get("reason", ""),
            mutated_params=mutated_params,
            sanitized_output=data.get("sanitized_output"),
            policy_matched=data.get("policy_matched") or None,
            session_risk=data.get("session_risk"),
            session_drift=data.get("session_drift"),
            data_ref_out=data.get("data_ref_out") or None,
        )


def _error_message(resp: requests.Response) -> str:
    try:
        body = resp.json()
        if isinstance(body, dict) and "error" in body:
            return str(body["error"])
    except ValueError:
        pass
    return resp.text[:200]
