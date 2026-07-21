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
5. **Check exit codes** — 0 = success, 1 = error, 2 = auth required, 3 = not found, 4 = conflict, 5 = forbidden (do not retry)

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

### Block an endpoint

```bash
onecli rules create --name "Block Gmail send" \
  --host-pattern "gmail.googleapis.com" \
  --path-pattern "/gmail/v1/users/me/messages/send" \
  --action block --method POST
```

### Rate limit an endpoint

```bash
onecli rules create --name "Limit Anthropic calls" \
  --host-pattern "api.anthropic.com" \
  --action rate_limit --rate-limit 100 --rate-limit-window hour
```

Cloud deployments reject legacy `rules` writes (410) — use the `policy`
family there. Pre-cutover self-hosted servers still accept legacy writes and
reject `policy` writes (403); org policy routes are Cloud/Enterprise-only.

### Manage policy-engine rules (cloud)

```bash
onecli policy rules create --name "Limit Anthropic calls" --action allow \
  --targets '[{"kind":"network","hostPattern":"api.anthropic.com"}]' \
  --rate-limit 100 --rate-limit-window hour
onecli policy status
```

Policy invariants (make these explicit — they are not intuitable):

- Writes land in a DRAFT and AUTO-PUBLISH only when the draft has no other
  staged changes; otherwise the publish is withheld (`publishSkipped` in the
  output) — review with `policy status`, then `policy publish` or re-run
  with `--publish-all`. A publish snapshots the WHOLE draft, including
  changes staged by other users.
- Enforced state = `policy rules list --status published`. Compare rules
  across draft/published by `logicalId`, NEVER by `id` (published row ids
  regenerate on every publish).
- `policy rules reorder --ordered-ids` must name EVERY non-default draft
  rule exactly once (including system-managed blocklist/equipment rows the
  web console hides) — take the full list from
  `policy rules list --quiet id` (the default is unlimited).
- Always use `--dry-run` first on mutating policy commands.
- On pre-cutover self-hosted servers, `policy rules list` shows a staging
  store that is NOT enforced (the legacy model still enforces there).

### Check agent configuration

```bash
onecli agents list --fields id,name,secretMode
onecli agents secrets --id <agent-id>
```

### View and regenerate API key

```bash
onecli auth api-key
onecli auth regenerate-api-key
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
