# OneCLI CLI

CLI for [OneCLI](https://onecli.sh) — manage agents, secrets, rules, and configuration from the terminal.

## Install

```bash
curl -fsSL onecli.sh/cli/install | sh
```

Or download from [GitHub Releases](https://github.com/onecli/onecli-cli/releases), or build from source:

```bash
go install github.com/onecli/onecli-cli/cmd/onecli@latest
```

## Quick Start

```bash
onecli auth login --api-key oc_...
onecli agents list
onecli secrets list
onecli agents create --name "My Agent" --identifier my-agent
```

## Commands

### Agents

```
onecli agents list                                     List all agents
onecli agents get-default                              Get the default agent
onecli agents create --name X --identifier Y           Create a new agent
onecli agents delete --id X                            Delete an agent
onecli agents rename --id X --name Y                   Rename an agent
onecli agents regenerate-token --id X                  Regenerate access token
onecli agents credentials --id X                       What the agent can use (read-only)
onecli agents set-secret-mode --id X --mode selective  Set secret mode
onecli agents secrets --id X                           RETIRED — see 'agents credentials'
onecli agents set-secrets --id X --secret-ids a,b      RETIRED — grant with a policy rule
```

### Secrets

```
onecli secrets list                                    List all secrets
onecli secrets create --name X --type anthropic ...    Create a new secret
onecli secrets update --id X --value Y                 Update a secret
onecli secrets delete --id X                           Delete a secret
```

### Rules (legacy model)

Cloud deployments reject these writes (410) — use the `policy` family below.
Pre-cutover self-hosted servers still accept them.

```
onecli rules list                                      RETIRED — see 'policy rules list'
onecli rules get --id X                                RETIRED — see 'policy rules get'
onecli rules create --name X --host-pattern Y ...      RETIRED — see 'policy rules create'
onecli rules update --id X [--action block] ...        RETIRED — see 'policy rules update'
onecli rules delete --id X                             RETIRED — see 'policy rules delete'
```

Every `rules` subcommand answers **410 Gone** on an updated server — the policy
engine replaced them. They remain only for pre-cutover self-hosted servers,
where `policy` is not yet available, and retire when those do.

### Policy reflections (read-only)

What the PUBLISHED policy actually allows — the replacements for the retired
permission and equipment reads. These resolve the rules, so a credential granted
by a rule shows up here even though no assignment row exists for it.

```
onecli policy effective-permissions --provider gmail        Per-tool verdicts for an app
onecli org policy effective-permissions --provider gmail    …at the org scope
onecli agents credentials --id X                            What one agent can use
onecli apps connections agent-access --id X                 Who can reach a connection
```

### Policy (the policy engine)

Rules stage into a DRAFT and enforce on publish. Writes auto-publish when the
draft has no other staged changes (`--no-publish` stages; `--publish-all`
publishes everything).

```
onecli policy rules list [--status published]          List rules (draft or the enforced set)
onecli policy rules get --id X                         Get a DRAFT rule
onecli policy rules create --name X --action allow \
  --targets '[{"kind":"network","hostPattern":"api.example.com"}]'
onecli policy rules update --id X [--action block]     Update a DRAFT rule
onecli policy rules delete --id X                      Delete a DRAFT rule
onecli policy rules reorder --ordered-ids '[...]'      Reorder (every draft id exactly once)
onecli policy default get                              Show the terminal Default Rule
onecli policy default set --action allow|block         Set the Default Rule's action
onecli policy publish                                  Publish the whole staged draft
onecli policy status                                   Staged diff + last publish
```

### Organization

Organization-level resources are shared by every project in the org. Authenticate with an organization API key (`oc_org_...`); project selection is not required.

```
onecli org secrets list|create|update|delete           Manage org-level secrets
onecli org rules list|get|create|update|delete         Manage org-level rules (legacy; cloud rejects writes)
onecli org rules permissions get|set --provider X      Layered app permissions (legacy; cloud rejects writes)
onecli org policy rules list|get|create|update|delete|reorder  Org policy-engine rules (draft → publish)
onecli org policy default get|set / publish / status   Org Default Rule + publish + staged diff
onecli org connections list [--provider X]             List org connections
onecli org connections rename --id X --label Y         Rename an org connection
onecli org connections delete --id X                   Delete an org connection
onecli org apps configured|get|configure|remove|toggle Manage org BYOC app credentials
onecli org apps connect --provider X --field k=v       Connect an app org-wide (API-key apps)
onecli org apps authorize --provider X                 Get the OAuth authorize URL (open in a browser)
onecli org apps blocklist list|activate|add|...        Manage org app blocklists
onecli org settings get|set                            Organization settings
```

### Auth

```
onecli auth login [--api-key oc_...]                   Store API key
onecli auth logout                                     Remove stored API key
onecli auth status                                     Check current auth state
```

Authentication is only required when the server enforces it. In local mode, commands work without logging in.

### Config

```
onecli config get <key>                                Read config value
onecli config set <key> <value>                        Write config value
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ONECLI_API_KEY` | API key (overrides stored key) |
| `ONECLI_API_HOST` | API base URL (default: `https://api.onecli.sh`) |
| `ONECLI_ENV` | `dev` or `production` |

## Output

All output is JSON. Filter with `--fields`, extract single values with `--quiet`:

```bash
onecli agents list --quiet id
onecli agents list --fields id,name,secretMode
```

## License

Apache 2.0
