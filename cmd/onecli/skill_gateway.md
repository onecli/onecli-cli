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

**For OAuth apps (Gmail, GitHub, Google Calendar, etc.):**

1. Get the `connect_url` from the error response
2. Always append `&source=agent&agent_name=` + URL-encoded `$ONECLI_AGENT_NAME` to the URL.
   Example: `http://localhost:10254/connections?connect=github&source=agent&agent_name=Claude%20Code`
3. Present the full URL to the user
4. **Immediately start polling.** Do NOT wait for the user to say "try now".
   Poll by checking the `connection` field in the JSON response:

```bash
# Check if connected (returns "connected" or "not_connected")
onecli apps get --provider gmail 2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('connection',{}).get('status','none') if d.get('connection') else 'not_connected')"
```

5. When the output shows `connected`, automatically retry your original
   request and continue the task
6. If still not connected after 2 minutes, ask the user if they need help

**For API key services (Stripe, custom APIs, etc.):**

Tell the user to add the secret:
   `onecli secrets create --name 'Stripe' --type generic --value 'sk_...' --host-pattern 'api.stripe.com'`
Then poll `onecli secrets list --fields hostPattern --quiet hostPattern` until
the host pattern appears.

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
