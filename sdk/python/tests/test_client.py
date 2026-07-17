import warnings

import pytest
import requests

from perchguard import Action, PerchGuardClient
from perchguard.exceptions import (
    PerchGuardAuthError,
    PerchGuardConfigError,
    PerchGuardUnavailableError,
    PerchGuardValidationError,
)


class FakeResponse:
    def __init__(self, status_code, json_body=None, text=""):
        self.status_code = status_code
        self._json = json_body
        self.text = text or ""

    def json(self):
        if self._json is None:
            raise ValueError("no json body")
        return self._json


class FakeHTTP:
    """Records calls and returns pre-programmed responses in order, or raises."""

    def __init__(self):
        self.calls = []
        self._post_responses = []
        self._delete_responses = []

    def queue_post(self, response_or_exc):
        self._post_responses.append(response_or_exc)

    def queue_delete(self, response_or_exc):
        self._delete_responses.append(response_or_exc)

    def post(self, url, json=None, headers=None, timeout=None):
        self.calls.append(("POST", url, json, headers))
        r = self._post_responses.pop(0)
        if isinstance(r, Exception):
            raise r
        return r

    def delete(self, url, headers=None, timeout=None):
        self.calls.append(("DELETE", url, None, headers))
        r = self._delete_responses.pop(0)
        if isinstance(r, Exception):
            raise r
        return r


def make_registration_response(session_id="pg-test-abc123", agent_id="test-agent"):
    return FakeResponse(
        201,
        {
            "agent_id": agent_id,
            "manifest_version": "0.1.0",
            "session_id": session_id,
            "token": "pgat-deadbeef",
            "effective_policies": ["ToolAuthorizationValidator"],
            "context_loaded": False,
            "parent_session_id": "",
        },
    )


# -- register ----------------------------------------------------------


def test_register_success():
    http = FakeHTTP()
    http.queue_post(make_registration_response())
    pg = PerchGuardClient("http://localhost:8080", http=http)

    session = pg.register(agent_id="test-agent", agent_role="developer_agent", intent="do the thing")

    assert session.id == "pg-test-abc123"
    assert session.agent_id == "test-agent"
    assert session.agent_role == "developer_agent"
    assert session.token == "pgat-deadbeef"
    assert session.intent == "do the thing"

    method, url, payload, _ = http.calls[0]
    assert method == "POST"
    assert url == "http://localhost:8080/agents/register"
    assert payload["mission"]["summary"] == "do the thing"
    assert payload["authorization"]["role"] == "developer_agent"


def test_register_validation_error():
    http = FakeHTTP()
    http.queue_post(FakeResponse(400, {"error": "manifest.mission.summary is required"}))
    pg = PerchGuardClient("http://localhost:8080", http=http)

    with pytest.raises(PerchGuardValidationError, match="summary is required"):
        pg.register(agent_id="a", agent_role="developer_agent", intent="")


def test_register_transport_failure():
    http = FakeHTTP()
    http.queue_post(requests.ConnectionError("refused"))
    pg = PerchGuardClient("http://localhost:8080", http=http)

    with pytest.raises(PerchGuardUnavailableError):
        pg.register(agent_id="a", agent_role="developer_agent", intent="x")


# -- intercept / validate_output ---------------------------------------


def _registered_client():
    http = FakeHTTP()
    http.queue_post(make_registration_response())
    pg = PerchGuardClient("http://localhost:8080", http=http)
    session = pg.register(agent_id="test-agent", agent_role="developer_agent", intent="task intent")
    return pg, http, session


def test_intercept_allow_attaches_token_header():
    pg, http, session = _registered_client()
    http.queue_post(FakeResponse(200, {"uid": "u1", "decision": "ALLOW", "reason": "all checks passed"}))

    decision = pg.intercept(session=session, uid="u1", tool_name="read_file", parameters={"path": "x"})

    assert decision.action == Action.ALLOW
    assert not decision.blocked
    _, _, payload, headers = http.calls[-1]
    assert payload["agent_role"] == "developer_agent"
    assert payload["user_intent"] == "task intent"
    assert headers["X-PerchGuard-Agent-Token"] == "pgat-deadbeef"


def test_intercept_deny_via_403_is_a_decision_not_an_exception():
    """The bug this SDK fixes: HTTP 403 on /intercept is a legitimate DENY/TERMINATE
    signal (see interceptor.go ServeHTTP), not a transport failure."""
    pg, http, session = _registered_client()
    http.queue_post(
        FakeResponse(403, {"uid": "u1", "decision": "DENY", "reason": "tool not authorized for role"})
    )

    decision = pg.intercept(session=session, uid="u1", tool_name="delete_all", parameters={})

    assert decision.action == Action.DENY
    assert decision.blocked


def test_intercept_mutate_parses_mutated_params():
    pg, http, session = _registered_client()
    http.queue_post(
        FakeResponse(
            200,
            {
                "uid": "u1",
                "decision": "MUTATE",
                "reason": "path sanitized",
                "mutated_call": {"name": "read_file", "parameters": {"path": "/safe/x"}},
            },
        )
    )

    decision = pg.intercept(session=session, uid="u1", tool_name="read_file", parameters={"path": "../x"})

    assert decision.action == Action.MUTATE
    assert decision.mutated_params == {"path": "/safe/x"}


def test_intercept_transport_failure_raises():
    pg, http, session = _registered_client()
    http.queue_post(requests.Timeout("timed out"))

    with pytest.raises(PerchGuardUnavailableError):
        pg.intercept(session=session, uid="u1", tool_name="read_file", parameters={})


def test_intercept_unexpected_status_raises():
    pg, http, session = _registered_client()
    http.queue_post(FakeResponse(500, {"error": "internal error"}))

    with pytest.raises(PerchGuardUnavailableError):
        pg.intercept(session=session, uid="u1", tool_name="read_file", parameters={})


def test_validate_output_mutate_returns_sanitized_output():
    pg, http, session = _registered_client()
    http.queue_post(
        FakeResponse(
            200,
            {
                "uid": "",
                "decision": "MUTATE",
                "reason": "PII redacted",
                "sanitized_output": "hello [REDACTED]",
            },
        )
    )

    decision = pg.validate_output(session=session, output="hello 123-45-6789")

    assert decision.action == Action.MUTATE
    assert decision.sanitized_output == "hello [REDACTED]"

    _, _, payload, headers = http.calls[-1]
    assert payload["agent_id"] == session.agent_id  # required for token verification server-side
    assert headers["X-PerchGuard-Agent-Token"] == session.token


# -- evict ----------------------------------------------------------


def test_evict_requires_api_key():
    pg, http, session = _registered_client()

    with pytest.raises(PerchGuardConfigError):
        pg.evict(session)


def test_evict_success_with_api_key():
    pg, http, session = _registered_client()
    http.queue_delete(FakeResponse(200, {"session_id": session.id, "status": "terminated"}))

    pg.evict(session, api_key="pgmk-secret")

    method, url, _, headers = http.calls[-1]
    assert method == "DELETE"
    assert url == f"http://localhost:8080/api/sessions/{session.id}"
    assert headers["Authorization"] == "Bearer pgmk-secret"


def test_evict_client_level_api_key():
    http = FakeHTTP()
    http.queue_post(make_registration_response())
    pg = PerchGuardClient("http://localhost:8080", http=http, api_key="pgmk-clientkey")
    session = pg.register(agent_id="a", agent_role="developer_agent", intent="x")
    http.queue_delete(FakeResponse(200, {}))

    pg.evict(session)  # no per-call api_key needed

    _, _, _, headers = http.calls[-1]
    assert headers["Authorization"] == "Bearer pgmk-clientkey"


def test_evict_auth_error():
    pg, http, session = _registered_client()
    http.queue_delete(FakeResponse(403, {"error": "invalid API key"}))

    with pytest.raises(PerchGuardAuthError):
        pg.evict(session, api_key="wrong-key")


def test_evict_404_is_not_an_error():
    pg, http, session = _registered_client()
    http.queue_delete(FakeResponse(404, {"error": "session not found"}))

    pg.evict(session, api_key="pgmk-secret")  # should not raise


def test_evict_best_effort_swallows_missing_api_key():
    pg, http, session = _registered_client()

    with warnings.catch_warnings(record=True) as w:
        warnings.simplefilter("always")
        pg.evict(session, best_effort=True)
        assert len(w) == 1


def test_session_context_manager_auto_evicts_best_effort():
    http = FakeHTTP()
    http.queue_post(make_registration_response())
    pg = PerchGuardClient("http://localhost:8080", http=http, api_key="pgmk-secret")

    with pg.register(agent_id="a", agent_role="developer_agent", intent="x") as session:
        http.queue_delete(FakeResponse(200, {}))
        assert session.id == "pg-test-abc123"

    method, url, _, _ = http.calls[-1]
    assert method == "DELETE"
    assert url.endswith(session.id)
