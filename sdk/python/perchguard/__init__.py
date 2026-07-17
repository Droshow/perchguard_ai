"""PerchGuard Python SDK.

Typed client for PerchGuard's agentic admission control API, plus an optional
high-level loop helper for the explicit-governance integration pattern.
See README.md in this directory for the two integration patterns this SDK
supports and does not support.
"""

from .client import PerchGuardClient
from .exceptions import (
    PerchGuardAuthError,
    PerchGuardConfigError,
    PerchGuardError,
    PerchGuardUnavailableError,
    PerchGuardValidationError,
)
from .loop import GovernedAgentLoop
from .mcp import MCPClient
from .types import Action, Decision, Session

__version__ = "0.1.0"

__all__ = [
    "PerchGuardClient",
    "GovernedAgentLoop",
    "MCPClient",
    "Action",
    "Decision",
    "Session",
    "PerchGuardError",
    "PerchGuardUnavailableError",
    "PerchGuardValidationError",
    "PerchGuardAuthError",
    "PerchGuardConfigError",
]
