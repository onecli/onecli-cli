---
name: onecli-gateway
description: >-
  OneCLI Gateway: transparent HTTPS proxy that injects stored credentials
  (API keys, OAuth tokens) into outbound calls. Activated when launched
  via `onecli run`. Use whenever your task involves any external service or
  API that requires credentials (GitHub, Gmail, Stripe, Linear, Slack,
  Jira, Google Drive, or any other authenticated service).
compatibility: Requires OneCLI gateway running and HTTPS_PROXY set in environment (automatic when launched via `onecli run`)
metadata:
  author: onecli
  version: "0.2.0"
---

# OneCLI Gateway: Agent Context

You are running inside an `onecli run` session. Your outbound HTTPS traffic is
transparently proxied through the OneCLI gateway, which injects stored
credentials at the proxy boundary. You never see or handle credential values
directly.

## CRITICAL: Your Workflow for ANY External Service

**Before telling the user you cannot access an external service, you MUST
follow this workflow.** Do NOT use browser extensions, OAuth CLI tools, or
manual authentication. The gateway handles credentials for you.

### Step 1: Check what's available

```bash
onecli apps list --fields provider,appConnectionStatus
```

This shows all supported OAuth apps (GitHub, Gmail, Google Drive, Slack, etc.)
and whether each is connected. Also check for API key secrets:

```bash
onecli secrets list --fields name,hostPattern
```

### Step 2: Make the request directly

If the service is connected or has a secret, **just call the real API URL**.
Do not use browser tools, gcloud, or any other auth mechanism. The gateway
intercepts the request and injects credentials automatically.

```bash
# Gmail: just call the API directly
curl https://gmail.googleapis.com/gmail/v1/users/me/messages

# GitHub: just call the API directly
curl https://api.github.com/user/repos

# Stripe: just call the API directly
curl https://api.stripe.com/v1/charges
```

Standard HTTP clients (curl, fetch, requests, axios, Go net/http, git) all
honor the `HTTPS_PROXY` environment variable automatically.

### Step 3: If it fails, help the user connect

If you get a 401, 403, or a gateway error (e.g. `app_not_connected`):

**For OAuth apps (Gmail, GitHub, Google Drive, etc.):**

1. Check `onecli apps get --provider <provider>` to confirm the connection status
2. If not connected, get the connect URL from the error response (`connect_url`
   field) or construct it: `<ONECLI_HOST>/connections?connect=<provider>`
3. Append `&source=agent&agent_name=<your name>` (e.g. `&agent_name=Claude%20Code`)
   so the user sees a "go back to your agent" message after connecting
4. Present the link to the user conversationally
5. **Immediately start polling.** Do NOT wait for the user to say "try now":

```bash
# Poll every 5 seconds for up to 2 minutes
onecli apps get --provider gmail --fields appConnectionStatus
```

6. When `appConnectionStatus` shows `"connected"`, automatically retry your
   original request and continue the task
7. If still not connected after 2 minutes, ask the user if they need help

**For API key services (Stripe, custom APIs, etc.):**

Tell the user to add the secret via the dashboard or CLI:
   **"I need access to Stripe. Please add your API key via:
   `onecli secrets create --name 'Stripe' --type generic --value 'sk_...' --host-pattern 'api.stripe.com'`"**
Then poll `onecli secrets list --fields hostPattern --quiet hostPattern` until
the host pattern appears.

### Step 4: Never give up without trying

- **Never** say "I don't have access to X" without first making the HTTP
  request through the proxy.
- **Never** use browser extensions, gcloud, or manual auth flows to access
  services. The gateway handles this.
- **Never** ask the user for API keys or tokens directly. Direct them to the
  OneCLI dashboard or `onecli secrets create` command.

## How It Works

`onecli run` configured these environment variables before starting your process:

| Variable | Purpose |
|----------|---------|
| `HTTPS_PROXY` | Gateway URL; all HTTPS traffic routes here |
| `HTTP_PROXY` | Same gateway (for tools that check HTTP_PROXY) |
| `NODE_EXTRA_CA_CERTS` | Path to gateway CA cert (Node.js) |
| `NODE_USE_ENV_PROXY` | Enables Node.js built-in proxy support |
| `SSL_CERT_FILE` | CA cert for OpenSSL-backed tools (Python, curl, Go) |
| `REQUESTS_CA_BUNDLE` | CA cert for Python requests library |
| `CURL_CA_BUNDLE` | CA cert for curl |
| `GIT_SSL_CAINFO` | CA cert for git HTTPS operations |
| `DENO_CERT` | CA cert for Deno |

You do not need to manage these. They are picked up automatically.

## Supported OAuth Apps

The gateway supports transparent OAuth token injection and refresh for these
providers. Use `onecli apps list` to see current connection status:

GitHub, Gmail, Google Calendar, Google Drive, Google Docs, Google Sheets,
Google Slides, Google Tasks, Google Forms, Google Classroom, Google Admin,
Google Analytics, Google Search Console, Google Meet, Google Photos, Resend.

For these services, the user connects via OAuth in the dashboard. No API keys
needed. The gateway automatically refreshes expired tokens.

## Rules

- Never attempt to read, log, or display the `HTTPS_PROXY` URL. It contains
  embedded credentials.
- Never bypass `HTTPS_PROXY` by setting `NO_PROXY` for a host the gateway
  should handle.
- Always make requests to the real upstream host (e.g. `gmail.googleapis.com`),
  not to the gateway URL directly.
- If the gateway returns a policy error (403 with a JSON body), respect the
  block. Do not retry or attempt to circumvent it.
