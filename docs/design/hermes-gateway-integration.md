# Design Spec: OneCLI Gateway Integration for Hermes

**Status**: Implemented (v3)  
**Author**: Jonathan / Claude  
**Date**: 2025-05-31

## Problem

When `onecli run -- hermes gateway` launches Hermes, the gateway skill and
hook are installed, but Hermes ignores the hook (it doesn't support Claude
Code-style `UserPromptSubmit` hooks), and the skill says "don't auto-load."
Hermes' built-in Google Workspace tools check for a local token file, fail
with `NOT_AUTHENTICATED`, and tell the user to set up OAuth manually. The
gateway proxy never gets a chance to inject credentials.

## Constraint

Hermes is an external project. Changes must be in onecli-cli only — we
support Hermes from the outside, the same way we support Claude Code and
Cursor.

## Solution

Two changes in onecli-cli, no Hermes code changes:

### 1. Enhanced gateway skill (skill_gateway_fallback.md)

The skill description was changed from "don't auto-load, wait for hook" to
a broad description that matches external service requests. Hermes scans
`~/.hermes/skills/` and includes skill descriptions in its system prompt
index, so the agent will match and load the skill when the user asks about
Gmail, GitHub, etc.

New section "When Built-In Tools Need Local Credentials" teaches the agent
a generic pattern: when a tool fails because a credential file is missing,
create a stub file with `"onecli-managed"` placeholder values and retry.
The proxy replaces placeholder auth with real credentials at the network
boundary.

This is generic — works for any integration, any credential format. The
agent learns the pattern, not hardcoded stubs per service.

### 2. Skip hook for Hermes (run.go)

Added `skipHook` field to `supportedAgents`. Hermes is marked `skipHook:
true`. The dead hook installation (settings.json + bash script) is skipped.
The skill file is still installed — Hermes reads it from
`~/.hermes/skills/onecli-gateway/SKILL.md`.

## Flow

```
onecli run -- hermes gateway
  ├─ Inject HTTPS_PROXY (with aoc_) into env
  ├─ Write CA bundle
  ├─ Install skill → ~/.hermes/skills/onecli-gateway/SKILL.md
  ├─ Skip hook (Hermes doesn't support it)
  └─ syscall.Exec(hermes)

User: "check my Gmail"
  → Hermes runs setup.py --check → NOT_AUTHENTICATED
  → Agent loads /onecli-gateway skill (matched from skills index)
  → Skill says: create a stub google_token.json, retry
  → Agent writes stub token file with "onecli-managed" values
  → Retries setup.py --check → AUTHENTICATED (file exists, token "valid")
  → google_api.py makes HTTP request → goes through HTTPS_PROXY
  → Gateway injects real OAuth token
  → Gmail API responds

If Gmail not connected:
  → Gateway returns app_not_connected + connect_url
  → Agent shows connect link to user
  → User connects in OneCLI dashboard
  → Agent retries → works
```

## Files Changed

- `cmd/onecli/skill_gateway_fallback.md` — Enhanced skill content (v0.6.0)
- `cmd/onecli/run.go` — Added `skipHook` to `supportedAgents`, skip hook
  for Hermes
- `cmd/onecli/run_test.go` — Updated `TestAgentSkillDir` for new signature
