"""Typed data model for the PerchGuard Python SDK.

These mirror the Go wire types in pkg/admission/types.go and pkg/api/agents.go —
see that package for the authoritative field semantics.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum
from typing import Any, Optional


class Action(str, Enum):
    """Mirrors admission.Decision in pkg/admission/types.go."""

    ALLOW = "ALLOW"
    DENY = "DENY"
    MUTATE = "MUTATE"
    HUMAN_REVIEW = "HUMAN_REVIEW"
    TERMINATE = "TERMINATE"


@dataclass
class Decision:
    """A typed port of ToolCallAdmissionResponse (pkg/admission/types.go)."""

    action: Action
    reason: str
    mutated_params: Optional[dict[str, Any]] = None
    sanitized_output: Optional[str] = None
    policy_matched: Optional[str] = None
    session_risk: Optional[float] = None
    session_drift: Optional[float] = None
    data_ref_out: Optional[str] = None

    @property
    def blocked(self) -> bool:
        """True when the caller must not proceed with the tool call/output as-is."""
        return self.action in (Action.DENY, Action.TERMINATE)


@dataclass
class Session:
    """A registered agent session, returned by PerchGuardClient.register().

    Supports `with pg.register(...) as session:` for automatic eviction —
    __exit__ calls back into the client that created it.
    """

    id: str
    agent_id: str
    agent_role: str
    manifest_version: str
    token: str
    intent: str = ""
    effective_policies: list[str] = field(default_factory=list)
    context_loaded: bool = False
    parent_session_id: str = ""
    _client: Optional["Any"] = field(default=None, repr=False, compare=False)

    def __enter__(self) -> "Session":
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        if self._client is not None:
            self._client.evict(self, best_effort=True)
