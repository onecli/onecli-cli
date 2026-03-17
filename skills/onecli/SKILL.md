# OneCLI CLI — Agent Context

## Quick Start

```bash
# Authenticate first
onecli auth login --api-key oc_...

# Verify authentication
onecli auth status
```

## Invariants

1. **Always authenticate first** — run `onecli auth login` before any other command
2. **Always `list` before acting** — get IDs from list commands, then pass explicit IDs
3. **Use `--fields` on list commands** — request only the fields you need
4. **Use `--dry-run` before mutating** — preview changes before applying them
5. **Check exit codes** — 0 = success, 1 = error, 2 = auth required, 3 = not found, 4 = conflict

## Common Workflows

### Set up a new agent with a secret

```bash
# 1. Create the secret
onecli secrets create --name "Anthropic Key" --type anthropic \
  --value "sk-ant-..." --host-pattern "api.anthropic.com"

# 2. Get the secret ID
onecli secrets list --quiet id

# 3. Create the agent
onecli agents create --name "My Agent" --identifier my-agent

# 4. Get the agent ID
onecli agents list --quiet id

# 5. Assign the secret to the agent
onecli agents set-secrets --id <agent-id> --secret-ids <secret-id>
```

### Check agent configuration

```bash
onecli agents list --fields id,name,secretMode
onecli agents secrets --id <agent-id>
```

## Output Format

All output is JSON. Errors go to stderr with this shape:

```json
{
  "error": "description",
  "code": "ERROR|AUTH_REQUIRED|NOT_FOUND|CONFLICT",
  "action": "suggested next command"
}
```

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `ONECLI_API_KEY` | API key (overrides stored key) |
| `ONECLI_API_HOST` | API base URL (overrides config) |
| `ONECLI_ENV` | `dev` or `production` |
