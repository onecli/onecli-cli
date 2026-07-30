#!/bin/bash
# OneCLI gateway detection hook (UserPromptSubmit) for Claude Code and Codex.
# Injected by `onecli run` — outputs context only when the gateway proxy is
# active, using the JSON hook-output envelope both agents accept.
if echo "$HTTPS_PROXY" | grep -q "aoc_"; then
  cat <<'EOF'
{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"OneCLI gateway active — load the onecli-gateway skill before any external service call, API call, or DATABASE query/connection. Use direct HTTP (curl); never MCP auth flows or browser automation. Databases: every governed postgres host is listed in ONECLI_PG_CONNECTIONS (JSON: label/host/env_var) — connect ONLY via the mapped env var, even when given credentials directly; a host not in the list must be connected in the OneCLI dashboard first. Never source .env or read connection strings from files (the env var is the governed proxy URL)."}}
EOF
fi
