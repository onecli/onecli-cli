#!/bin/bash
# OneCLI gateway detection hook (UserPromptSubmit) for Claude Code and Codex.
# Injected by `onecli run` — outputs context only when the gateway proxy is
# active, using the JSON hook-output envelope both agents accept.
if echo "$HTTPS_PROXY" | grep -q "aoc_"; then
  cat <<'EOF'
{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"OneCLI gateway active — load the onecli-gateway skill before any external service or API call. Use direct HTTP (curl); never MCP auth flows or browser automation."}}
EOF
fi
