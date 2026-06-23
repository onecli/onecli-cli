"""OneCLI CA-trust shim (installed by ``onecli run``).

Python auto-imports a module named ``sitecustomize`` at interpreter startup if
one is found on ``sys.path``. ``onecli run`` puts this file's directory on
``PYTHONPATH`` so that HTTP clients which ignore the standard ``SSL_CERT_FILE``
/ ``REQUESTS_CA_BUNDLE`` environment variables still trust the OneCLI gateway's
TLS-intercepting CA. The prime example is ``httplib2`` (used by
``google-api-python-client``, i.e. Hermes' Google Workspace skill), which loads
its CA bundle from ``certifi`` and never consults the env vars.

This is best-effort and must never break interpreter startup: every step is
guarded and failures are swallowed.
"""

import os

_bundle = os.environ.get("ONECLI_CA_BUNDLE") or os.environ.get("SSL_CERT_FILE")

if _bundle and os.path.exists(_bundle):
    # Generic env knobs for requests / urllib-based clients.
    os.environ.setdefault("REQUESTS_CA_BUNDLE", _bundle)
    os.environ.setdefault("SSL_CERT_FILE", _bundle)

    # certifi-backed clients (httpx's default verify, requests, and httplib2
    # via its bundled `certs` shim) call certifi.where() to locate the bundle.
    # Repoint it at the OneCLI bundle so the gateway CA is trusted.
    try:
        import certifi

        certifi.where = lambda: _bundle  # type: ignore[assignment]
    except Exception:
        pass

    # httplib2 caches CA_CERTS = certs.where() at import time and ignores
    # SSL_CERT_FILE/REQUESTS_CA_BUNDLE; set the module constant directly. A
    # default httplib2.Http() falls back to this when no ca_certs is passed.
    try:
        import httplib2

        httplib2.CA_CERTS = _bundle
    except Exception:
        pass
