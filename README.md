# OneCLI CLI

CLI for [OneCLI](https://onecli.sh) — manage agents, secrets, and configuration from the terminal.

## Install

Download from [GitHub Releases](https://github.com/onecli/onecli-cli/releases), or build from source:

```bash
go install github.com/onecli/onecli-cli/cmd/onecli@latest
```

## Quick Start

```bash
onecli agents list
onecli secrets list
onecli agents create --name "My Agent" --identifier my-agent
```

If the server requires authentication:

```bash
onecli auth login --api-key oc_...
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
onecli agents secrets --id X                           List assigned secrets
onecli agents set-secrets --id X --secret-ids a,b      Set assigned secrets
onecli agents set-secret-mode --id X --mode selective  Set secret mode
```

### Secrets

```
onecli secrets list                                    List all secrets
onecli secrets create --name X --type anthropic ...    Create a new secret
onecli secrets update --id X --value Y                 Update a secret
onecli secrets delete --id X                           Delete a secret
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
| `ONECLI_API_HOST` | API base URL (default: `https://app.onecli.sh`) |
| `ONECLI_ENV` | `dev` or `production` |

## Output

All output is JSON. Filter with `--fields`, extract single values with `--quiet`:

```bash
onecli agents list --quiet id
onecli agents list --fields id,name,secretMode
```

## License

Apache 2.0
