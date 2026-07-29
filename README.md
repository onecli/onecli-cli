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
onecli agents list [--with-grants]                     List all agents (optionally with grants)
onecli agents get-default                              Get the default agent
onecli agents create --name X --identifier Y           Create a new agent
onecli agents delete --id X                            Delete an agent
onecli agents rename --id X --name Y                   Rename an agent
onecli agents regenerate-token --id X                  Regenerate access token
onecli agents credentials --id X                       What the agent can use (read-only)
onecli agents set-secret-mode --id X --mode selective  RETIRED — see 'agents grants'
onecli agents secrets --id X                           RETIRED — see 'agents grants list'
onecli agents set-secrets --id X --secret-ids a,b      RETIRED — see 'agents grants attach-secret'
```

### Grants (the attach model)

A grant attaches one credential to one agent — the only project-scope policy
write. An agent starts with nothing and uses exactly what its grants attach.
Grants are attach INTENT; `agents credentials` is the EFFECTIVE view with
organization guardrails applied. Changes publish immediately.

```
onecli agents grants list --id X                       The agent's attached connections + secrets
onecli agents grants attach-connection --id X \
  --connection-id Y [--allow t1,t2] [--ask t3]         Attach (no flags = full access)
onecli agents grants detach-connection --id X --connection-id Y
onecli agents grants attach-secret --id X --secret-id Y
onecli agents grants detach-secret --id X --secret-id Y
onecli apps connections grants --id Y                  Which agents a connection is granted to
```

Tool IDs come from `onecli apps permission-definition --provider <app>`. A tool
cannot be in both `--allow` and `--ask`; a grant with every tool set to Never is
a detach (the server rejects it). `--ask` requires the approvals feature.

### Secrets

```
onecli secrets list                                    List all secrets
onecli secrets create --name X --type anthropic ...    Create a new secret
onecli secrets update --id X --value Y                 Update a secret
onecli secrets delete --id X                           Delete a secret
```

### Rules (legacy model)

Updated servers answer **410 Gone** for every `rules` subcommand. Project access
is granted per agent (`agents grants`); org guardrails live under `org policy`.
The commands remain only for pre-cutover self-hosted servers and retire when
those do.

```
onecli rules list                                      RETIRED — see 'agents grants'
onecli rules get --id X                                RETIRED — see 'agents grants'
onecli rules create --name X --host-pattern Y ...      RETIRED — see 'agents grants attach-connection'
onecli rules update --id X [--action block] ...        RETIRED — see 'agents grants attach-connection'
onecli rules delete --id X                             RETIRED — see 'agents grants detach-connection'
```

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

### Policy (project scope — RETIRED)

Updated servers answer **410 Gone** for project-scope policy authoring: project
rules are compiled from agent credential grants, never authored directly. Use
`agents grants` for project access and `org policy` (below, under Organization)
for org guardrails. The commands remain only for self-hosted servers that
predate the attach model.

```
onecli policy rules ...                                RETIRED — see 'agents grants'
onecli policy default ...                              RETIRED — see 'org policy default'
onecli policy publish                                  RETIRED — grants publish immediately
onecli policy status                                   RETIRED — see 'agents grants list'
onecli policy effective-permissions --provider gmail   Per-tool verdicts (still live, read-only)
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
