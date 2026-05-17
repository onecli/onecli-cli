#!/bin/bash
# OneCLI gateway detection hook for Claude Code.
# Injected by `onecli run` — outputs context only when the gateway proxy is active.
if echo "$HTTPS_PROXY" | grep -q "aoc_"; then
  echo "OneCLI gateway is active. Load /onecli-gateway for any external service access (email, calendar, GitHub, Slack, Stripe, databases, APIs). Never use browser automation or MCP auth flows."
fi
