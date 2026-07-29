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

# 5. Attach the secret to the agent (the attach model's write path)
onecli agents grants attach-secret --id <agent-id> --secret-id <secret-id>
```

### Grant credentials to an agent (the attach model)

Project access is granted per agent. A grant attaches one credential; the
server compiles grants into policy rules — you never author project rules.

```bash
# Attach an app connection with full access (every catalog tool)
onecli agents grants attach-connection --id <agent-id> --connection-id <conn-id>

# Attach with per-tool access: --allow always runs, --ask needs approval,
# everything else is blocked. Tool IDs come from the app's catalog:
onecli apps permission-definition --provider gmail
onecli agents grants attach-connection --id <agent-id> --connection-id <conn-id> \
  --allow search_messages,get_message --ask send_email

# Attach a secret or LLM key (no per-tool axis)
onecli agents grants attach-secret --id <agent-id> --secret-id <secret-id>

# Read the attach list, and the whole project at once
onecli agents grants list --id <agent-id>
onecli agents list --with-grants
```

Grant invariants (make these explicit — they are not intuitable):

- Changes publish IMMEDIATELY — there is no draft or publish step at project
  scope.
- A tool cannot be in both `--allow` and `--ask` (422). A grant with every
  tool set to Never is a detach — the server rejects it (422); run
  `agents grants detach-connection` instead.
- `--ask` (approval-gated tools) requires the approvals feature — a 403
  names the plan; do not retry.
- `agents grants list` is attach INTENT; `agents credentials` is the
  EFFECTIVE view with organization guardrails applied. When they disagree,
  an org rule is capping the grant.
- Always use `--dry-run` first on mutating grant commands.

### Block or rate-limit an endpoint (organization rules)

Org-wide guardrails are policy rules, authored by org admins with an
organization API key:

```bash
onecli org policy rules create --name "Block Gmail send" --action block \
  --targets '[{"kind":"network","hostPattern":"gmail.googleapis.com","pathPattern":"/gmail/v1/users/me/messages/send","method":"POST"}]'
onecli org policy rules create --name "Limit Anthropic calls" --action allow \
  --rate-limit 100 --rate-limit-window hour \
  --targets '[{"kind":"network","hostPattern":"api.anthropic.com"}]'
onecli org policy status
```

Org policy invariants:

- Org writes land in a DRAFT and AUTO-PUBLISH only when the draft has no
  other staged changes; otherwise the publish is withheld (`publishSkipped`
  in the output) — review with `org policy status`, then `org policy publish`
  or re-run with `--publish-all`.
- Enforced state = `org policy rules list --status published`. Compare rules
  across draft/published by `logicalId`, NEVER by `id`.
- `org policy rules reorder --ordered-ids` must name EVERY non-default draft
  rule exactly once.

Retired surfaces (updated servers answer 410 Gone): the legacy `rules` and
`org rules` families, `org settings`, `agents set-secret-mode` /
`set-secrets` / `connections set`, and ALL project-scope `policy` writes
(`policy rules|default|publish|status`) — project rules are compiled from
grants.

### Multiple accounts of one app (gateway 409 protocol)

When an agent's request could be served by two or more attached accounts of
the same app, the gateway answers `409` with a self-describing JSON body:

```json
{
  "error": "multiple_connections",
  "connections": [{"id": "conn_abc", "label": "Work", "provider": "gmail"}],
  "header": "x-onecli-connection-id",
  "example": "x-onecli-connection-id: conn_abc"
}
```

Retry the identical request with the `x-onecli-connection-id` header set to
one of the listed ids (pick by label/context, or ask the user). Successful
responses advertise the choice list in the `x-onecli-connections` response
header. A `404 connection_not_found` means the chosen id is stale — re-pick
from its `connections` list. Permissions are per account: the same tool can
be allowed on one account and blocked on another. Connection ids also come
from `onecli apps connections list`.

### Check agent configuration

```bash
onecli agents list --with-grants
onecli agents grants list --id <agent-id>   # attach intent
onecli agents credentials --id <agent-id>   # effective (org guardrails applied)
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
