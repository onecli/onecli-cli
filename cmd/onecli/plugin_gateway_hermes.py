"""OneCLI Gateway plugin for Hermes.

Intercepts tool results containing auth errors (NOT_AUTHENTICATED, missing
token, etc.) and appends gateway recovery instructions so the agent creates
credential stubs instead of following manual OAuth/API-key setup flows.
"""

import json
import logging
import os
from typing import Any, Optional

log = logging.getLogger("onecli-gateway")

_AUTH_PATTERNS = [
    "not_authenticated",
    "no token at",
    "missing token",
    "oauth_client_disabled",
    "token_revoked",
    "token_invalid",
    "token_corrupt",
    "credentials not found",
    "authentication required",
    "not authenticated",
]

_RECOVERY_HINT = """

=== OneCLI Gateway: Auth Error Recovery ===

This auth error is handled by the OneCLI gateway proxy.
Do NOT follow OAuth setup, API key, or App Password instructions.

STEP 1 — Create a stub credential file at the path mentioned in the error.

  For Google tokens (google_token.json):
    echo '{"type":"authorized_user","access_token":"onecli-managed","refresh_token":"onecli-managed","client_id":"onecli-managed","client_secret":"onecli-managed","token_uri":"https://oauth2.googleapis.com/token","expiry":"2099-01-01T00:00:00+00:00"}' > /root/.hermes/google_token.json && chmod 600 /root/.hermes/google_token.json

  For other credentials:
    echo '{"api_key":"onecli-managed"}' > <path> && chmod 600 <path>

STEP 2 — Retry the failed operation. The gateway replaces "onecli-managed"
placeholders with real credentials at the network level.

STEP 3 — If the retry returns app_not_connected, show the user the
connect_url from the error response so they can connect the service.

=== End OneCLI Gateway ==="""


def _is_gateway_active():
    proxy = os.environ.get("HTTPS_PROXY", "") + os.environ.get("https_proxy", "")
    active = "aoc_" in proxy or os.environ.get("ONECLI_GATEWAY") == "true"
    return active


def _result_to_str(result):
    """Convert result to a searchable string regardless of type."""
    if isinstance(result, str):
        return result
    if isinstance(result, dict):
        return json.dumps(result, default=str)
    return str(result) if result is not None else ""


def _has_auth_error(text):
    lower = text.lower()
    return any(p in lower for p in _AUTH_PATTERNS)


def _on_transform_tool_result(
    tool_name: str = "",
    args: Any = None,
    result: Any = None,
    **_: Any,
) -> Optional[str]:
    if not _is_gateway_active():
        return None
    text = _result_to_str(result)
    if not _has_auth_error(text):
        return None
    log.warning("OneCLI gateway intercepted auth error in %s, injecting recovery hint", tool_name)
    if isinstance(result, str):
        return result + _RECOVERY_HINT
    return text + _RECOVERY_HINT


def register(ctx) -> None:
    log.info("OneCLI gateway plugin registered (transform_tool_result)")
    ctx.register_hook("transform_tool_result", _on_transform_tool_result)
