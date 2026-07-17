"""Typed exceptions for the PerchGuard Python SDK.

These exist so caller code can distinguish "PerchGuard is unreachable" from
"PerchGuard made a governance decision" — the two used to collapse into the
same bare TERMINATE string in admission_client.py.
"""


class PerchGuardError(Exception):
    """Base class for all SDK errors."""


class PerchGuardUnavailableError(PerchGuardError):
    """Raised when PerchGuard could not be reached or returned a non-decision error.

    Distinct from a TERMINATE decision, which means PerchGuard was reached and
    chose to end the session for cause. This means PerchGuard's answer is unknown.
    """


class PerchGuardValidationError(PerchGuardError):
    """Raised when PerchGuard rejected a request as malformed (HTTP 400)."""


class PerchGuardAuthError(PerchGuardError):
    """Raised when an operator-authenticated call (e.g. evict) is missing or has
    an invalid API key.
    """


class PerchGuardConfigError(PerchGuardError):
    """Raised for SDK misuse that has nothing to do with the network call —
    e.g. calling evict() without an api_key configured anywhere.
    """
