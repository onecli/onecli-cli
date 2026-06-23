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

# Specific, low-false-positive markers — safe to match even on a successful
# result, since they almost never appear in benign tool output.
_AUTH_PATTERNS_STRICT = [
    "not_authenticated",
    "no token at",
    "oauth_client_disabled",
    "token_revoked",
    "token_invalid",
    "token_corrupt",
    "refresh_failed",
]

# Looser natural-language phrases — only trusted when the tool actually errored,
# so a successful result that merely mentions them (an email subject, a web
# page) isn't rewritten with the recovery hint.
_AUTH_PATTERNS_LOOSE = [
    "not authenticated",
    "authentication required",
    "credentials not found",
    "missing token",
]

_RECOVERY_HINT = """

=== OneCLI Gateway: Auth Error Recovery ===

This auth error is handled by the OneCLI gateway proxy.
Do NOT follow OAuth, API-key, or App-Password setup instructions.

STEP 1 — Create a stub credential file at the EXACT path named in the error
above (e.g. the path after "No token at ..."). Use "onecli-managed" for every
secret value and match the format the tool expects. Examples (substitute the
real path from the error for <PATH>):

  Google OAuth token file:
    echo '{"type":"authorized_user","access_token":"onecli-managed","refresh_token":"onecli-managed","client_id":"onecli-managed","client_secret":"onecli-managed","token_uri":"https://oauth2.googleapis.com/token","expiry":"2099-01-01T00:00:00+00:00"}' > "<PATH>" && chmod 600 "<PATH>"

  Generic JSON credential / API-key file:
    echo '{"api_key":"onecli-managed"}' > "<PATH>" && chmod 600 "<PATH>"

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


def _looks_like_error(status, error_type, error_message):
    """Best-effort: did this tool call actually fail? Hermes passes these
    fields to transform_tool_result; older versions may not, in which case we
    fall back to strict-pattern matching only."""
    if error_type or error_message:
        return True
    if isinstance(status, str) and status.lower() not in (
        "",
        "ok",
        "success",
        "succeeded",
        "completed",
    ):
        return True
    return False


def _has_auth_error(text, is_error):
    lower = text.lower()
    if any(p in lower for p in _AUTH_PATTERNS_STRICT):
        return True
    if is_error and any(p in lower for p in _AUTH_PATTERNS_LOOSE):
        return True
    return False


def _on_transform_tool_result(
    tool_name: str = "",
    args: Any = None,
    result: Any = None,
    status: Any = None,
    error_type: Any = None,
    error_message: Any = None,
    **_: Any,
) -> Optional[str]:
    if not _is_gateway_active():
        return None
    text = _result_to_str(result)
    is_error = _looks_like_error(status, error_type, error_message)
    if not _has_auth_error(text, is_error):
        return None
    log.warning("OneCLI gateway intercepted auth error in %s, injecting recovery hint", tool_name)
    if isinstance(result, str):
        return result + _RECOVERY_HINT
    return text + _RECOVERY_HINT


def register(ctx) -> None:
    log.info("OneCLI gateway plugin registered (transform_tool_result)")
    ctx.register_hook("transform_tool_result", _on_transform_tool_result)
