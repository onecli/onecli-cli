# Design Spec: OneCLI Gateway Integration for Hermes

**Status**: Implemented (v4)
**Date**: 2026-06

## Problem

`onecli run -- hermes [...]` should let [Hermes](https://github.com/NousResearch/hermes-agent)
use the OneCLI gateway as transparently as Claude Code, Codex, and Cursor. Hermes
is harder than those because of how it is built:

1. **No Claude-style hooks.** Hermes ignores `UserPromptSubmit` hooks /
   `settings.json`, so the gateway-detection hook is dead weight for it.
2. **Tools run in a separate Docker sandbox.** With `terminal.backend: docker`,
   Hermes runs `terminal` / `execute_code` / file tools inside one persistent
   container that inherits **none** of the `onecli run` process environment — so
   the proxy + CA we set on the Hermes process never reach where tool HTTP
   actually happens. (Default backend is `local`, where tools run on the host
   and *do* inherit the env.)
3. **The Google Workspace skill uses httplib2.** `google-api-python-client`
   (httplib2 `0.31.2`) loads its CA bundle from `certifi` and **ignores**
   `SSL_CERT_FILE` / `REQUESTS_CA_BUNDLE`, so it rejects the gateway's
   TLS-intercepting CA. (It *does* honor the proxy: `proxy_info_from_environment`
   reads both `https_proxy` and `HTTPS_PROXY`, which OneCLI sets.)
4. **Hermes runs its own inference.** Its OpenAI-compatible client
   (`httpx 0.28.1`) honors `HTTPS_PROXY` and `SSL_CERT_FILE`, so its model calls
   already traverse — and can be governed by — the gateway.

## Constraint

Hermes is an external project. All changes live in onecli-cli; we support Hermes
from the outside, using only its public configuration seams (env vars, the
plugin directory, the skills directory).

## Solution

Five cooperating pieces, each mapped to a Hermes seam. All are gated on the
`hermes` entry in `supportedAgents` (`pluginGateway`, `dockerSandbox` flags).

### 1. Skill — autonomous guidance (`skill_gateway_fallback.md` + cloud)

Hermes indexes `~/.hermes/skills/*/SKILL.md` name+descriptions into its system
prompt (`skills_list`/`skill_view`), so a broad description makes the agent load
the skill when it hits an auth error. The OneCLI API serves a broad variant for
non-hook agents (`GET /v1/skill/gateway?agent_framework=hermes`); the embedded
`skill_gateway_fallback.md` is the offline fallback. The skill teaches the
generic "create an `onecli-managed` stub at the path the tool wants, then retry"
pattern.

### 2. Plugin — deterministic recovery (`plugin_gateway_hermes.*`)

Hermes' analogue of the Claude hook is a `transform_tool_result` plugin. It runs
in the agent process, matches auth-error patterns (e.g. `NOT_AUTHENTICATED`,
`No token at`) in any tool result, and appends recovery instructions so the
agent makes a stub instead of running a manual OAuth flow. The plugin is a
directory (`plugin.yaml` + `__init__.py:register(ctx)`) installed to
`~/.hermes/plugins/onecli-gateway/`. Plugins are **opt-in**, so we enable it by
adding `onecli-gateway` to `plugins.enabled` in `config.yaml` via a YAML
round-trip (`enableHermesPlugin`) that preserves the user's other settings,
keys, and comments.

### 3. Sandbox plumbing — `TERMINAL_DOCKER_*` env (`hermesSandboxEnv`)

Instead of editing `config.yaml`, we set Hermes' documented env overrides on the
child process (merged with any values already in the file):

- `TERMINAL_DOCKER_ENV` — proxy URL (see #5), CA paths (`/etc/ssl/onecli-ca.pem`),
  `PYTHONPATH` (the shim, see #4), `ONECLI_GATEWAY=true`.
- `TERMINAL_DOCKER_VOLUMES` — mount the CA bundle and the shim dir read-only.
- `TERMINAL_DOCKER_EXTRA_ARGS` — `--add-host host.docker.internal:host-gateway`
  on Linux, only when the sandbox reaches the gateway via `host.docker.internal`
  (see "Container-reachable proxy URL").
- `TERMINAL_DOCKER_PERSIST_ACROSS_PROCESSES=false` — Hermes reuses sandbox
  containers by label and ignores env/mount changes on reuse, so we disable
  cross-process reuse to guarantee a fresh container that picks up the proxy +
  CA. (On-disk filesystem persistence is unaffected.) This replaces the old
  `docker rm -f` cleanup.

These keys are inert when `terminal.backend` is `local`, so we set them
unconditionally for Hermes.

### 4. CA shim — trust the gateway CA from certifi/httplib2 (`sitecustomize_onecli_ca.py`)

A tiny `sitecustomize.py` is installed to `~/.onecli/pyca/` and put on
`PYTHONPATH` (host child env for the `local` backend; `TERMINAL_DOCKER_ENV` +
a volume mount for the `docker` backend). Python auto-imports it at startup; it
repoints `certifi.where()` and `httplib2.CA_CERTS` at the OneCLI combined bundle
(`ONECLI_CA_BUNDLE`). This makes httplib2 (Google Workspace) — and any other
certifi-pinned Python client — trust the gateway. It is best-effort and never
breaks interpreter startup.

### 5. Inference governance

Hermes' inference already flows through the gateway (httpx honors `HTTPS_PROXY`
+ `SSL_CERT_FILE`), so OneCLI can apply policy/metering to model calls. `onecli
run` prints a notice; the user must allow their model-provider host under a
deny-by-default policy. (For full key governance, point Hermes at a placeholder
key for an OneCLI-known provider so the gateway injects an OneCLI-managed LLM
secret — opt-in.) We also set `HERMES_CA_BUNDLE` so Hermes' own auth/portal
clients trust the gateway CA.

### Container-reachable proxy URL

The gateway lives at the host this process resolves it to (`gatewayHost` — e.g.
`api.onecli.sh` for cloud, or `127.0.0.1` for a self-hosted local gateway). A
container can reach a **routable** host directly, so the sandbox proxy uses
`gatewayHost` as-is. Only when `gatewayHost` is a **loopback** address (which a
container can't reach) do we swap it for `host.docker.internal` and add
`--add-host` on Linux. The proxy URL's credentials/port are captured **before**
`rewriteProxyEnvHosts` mutates `cfg.Env` (`containerProxyURLFor`). Forcing
`host.docker.internal` unconditionally would break every non-local deployment
(the container would proxy to the user's own machine), so it is conditional.

## Flow (docker backend)

```
onecli run -- hermes
  ├─ Fetch container-config → HTTPS_PROXY (+ lowercase), CA bundle
  ├─ Derive sandbox proxy URL (gatewayHost, or host.docker.internal if loopback)
  ├─ Write combined CA bundle → ~/.onecli/ca-bundle.pem
  ├─ Install skill → ~/.hermes/skills/onecli-gateway/SKILL.md
  ├─ Skip hook (Hermes ignores it)
  ├─ Install + enable plugin (~/.hermes/plugins/onecli-gateway, plugins.enabled)
  ├─ Install CA shim → ~/.onecli/pyca/sitecustomize.py
  ├─ Set HERMES_CA_BUNDLE + TERMINAL_DOCKER_ENV/VOLUMES/EXTRA_ARGS + persist=false
  └─ syscall.Exec(hermes)

User: "check my Gmail"
  → setup.py --check (in sandbox) → NOT_AUTHENTICATED: No token at <PATH>
  → transform_tool_result plugin appends recovery hint to the tool result
  → Agent writes an "onecli-managed" stub at <PATH>, retries → AUTHENTICATED
  → google_api.py (httplib2) → proxy via https_proxy; CA trusted via the shim
  → Gateway injects the real OAuth token → Gmail responds
  (If not connected: gateway returns app_not_connected + connect_url → shown)
```

## Files changed (onecli-cli)

- `cmd/onecli/run.go` — `agentSpec`/`supportedAgents`; `applyHermesGateway`,
  `hermesSandboxEnv`, `enableHermesPlugin` (+ yaml helpers), `installCAShim`,
  `proxyURLWithHost`/`firstProxyURL`, `prependPythonPath`; removed the
  config.yaml string-surgery and `removeStaleAgentContainers`.
- `cmd/onecli/plugin_gateway_hermes.py` — path-agnostic recovery hint.
- `cmd/onecli/plugin_gateway_hermes.yaml` — valid manifest (`provides_hooks`).
- `cmd/onecli/sitecustomize_onecli_ca.py` — new CA-trust shim (embedded).
- `cmd/onecli/skill_gateway_fallback.md` — broad description + stub guidance.
- `cmd/onecli/run_test.go` — `agentSpec` table + builder/merge/YAML tests.
- `go.mod` — `gopkg.in/yaml.v3`.

Server side (onecli-cloud `packages/api`): `GET /v1/skill/gateway` serves the
broad vs hook-based skill by `agent_framework` (shared/OSS file — sync upstream).

## Known limitations / verification

- The CA shim must bite before httplib2 builds its `Http()` — smoke-tested by
  `$GSETUP --check` → stub → `$GAPI gmail search` returning data.
- The `--add-host` is Linux-only; macOS/Windows Docker Desktop resolve
  `host.docker.internal` natively.
- Custom sandbox images that aren't root and set a non-default `HERMES_HOME`
  move the token path; the recovery hint uses the path named in the error, and
  the shim reads `ONECLI_CA_BUNDLE`, so both follow the actual environment.
