---
name: onecli-gateway
description: >-
  OneCLI Gateway: transparent HTTPS proxy that injects stored credentials
  into outbound calls. You MUST use this skill when the user asks you to
  read emails, check calendar, access GitHub repos, create issues, check
  Stripe payments, or interact with ANY external service or API. Do NOT
  use browser extensions or OAuth CLI tools. Make HTTP requests directly;
  the gateway injects credentials automatically.
compatibility: Requires HTTPS_PROXY set in environment (automatic when launched via `onecli run`)
metadata:
  author: onecli
  version: "0.4.0"
---

# OneCLI Gateway: Agent Context

You are running inside an `onecli run` session. Your outbound HTTPS traffic is
transparently proxied through the OneCLI gateway, which injects stored
credentials at the proxy boundary. You never see or handle credential values
directly.

## CRITICAL: How to Access External Services

**You have direct HTTP access to external APIs.** When the user asks you to
read emails, check their calendar, list GitHub repos, or interact with any
external service, make the HTTP request directly. Do NOT use browser
extensions, gcloud, or any other auth mechanism.

{{SERVICES_SECTION}}

## Making Requests

Just call the real API URL. The gateway intercepts the request and injects
credentials automatically.

```bash
curl -s "https://gmail.googleapis.com/gmail/v1/users/me/messages?maxResults=5"
curl -s "https://api.github.com/user/repos?per_page=10"
curl -s "https://api.stripe.com/v1/charges?limit=5"
```

Standard HTTP clients (curl, fetch, requests, axios, Go net/http, git) all
honor the `HTTPS_PROXY` environment variable automatically. You do not need
to set any auth headers.

## If a Request Fails

If you get a 401, 403, or a gateway error (e.g. `app_not_connected`):

### Step 1 — MANDATORY: Show the user a connect link

You MUST show the user a clickable link BEFORE doing anything else.
Do NOT skip this step. Do NOT jump to polling. The user cannot connect
without seeing this link.

**For OAuth apps:** use the `connect_url` from the error response directly:

> To connect [service], open this link:
> [connect_url from the error response]

**For API key services:** construct a pre-filled dashboard link:

> To add your [service] API key, open this link:
> {{ONECLI_URL}}/secrets/new?name=[service]&hostPattern=[api.host.com]&source=agent&agent_name={{AGENT_NAME_ENCODED}}

Do NOT show `onecli secrets create` CLI commands. Always use the dashboard link.

### Step 2 — ONLY after showing the link: Start polling

After the link is visible to the user, start polling in the background.
Do NOT wait for the user to say "try now" or "done".

For OAuth apps:
```bash
onecli apps get --provider [provider] 2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('connection',{}).get('status','none') if d.get('connection') else 'not_connected')"
```

For API key services:
```bash
onecli secrets list --fields hostPattern --quiet hostPattern
```

### Step 3 — Auto-retry

When the connection or secret appears, automatically retry your original
request and continue the task. If still not connected after 2 minutes,
ask the user if they need help.

## Rules

- **Never** say "I don't have access to X" without first making the HTTP
  request through the proxy.
- **Never** use browser extensions, gcloud, or manual auth flows. The gateway
  handles credentials for you.
- **Never** ask the user for API keys or tokens directly. Direct them to the
  OneCLI dashboard or `onecli secrets create`.
- **Never** suggest the user open Gmail/Calendar/GitHub in their browser
  when they ask you to read or interact with those services. You have API
  access. Use it.
- Never attempt to read, log, or display the `HTTPS_PROXY` URL. It contains
  embedded credentials.
- If the gateway returns a policy error (403 with a JSON body), respect the
  block. Do not retry or circumvent it.
