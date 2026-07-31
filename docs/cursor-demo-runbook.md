# OneCLI — Cursor coverage demo runbook

Last verified: 2026-07-31 (all checks below were run and observed, not assumed).

---

## The claim you can make

> "OneCLI governs Cursor coding agents at the OS level. Not settings the app can
> ignore — kernel rules it cannot override. Both surfaces: the terminal agent and
> the IDE itself."

---

## Before the demo

1. **Merge + deploy** `onecli-cloud` PR #754, and set on the gateway:
   ```
   GATEWAY_TUNNEL_HOSTS=*.cursor.sh
   ```
   Without this, Cursor's in-IDE AI chat errors (its gRPC/HTTP-2 traffic cannot
   traverse the HTTP/1.1-only MITM). Everything else works without it.

   Use the wildcard, not an enumerated host list. Tried and failed:
   `api2,api3,api4,repo42.cursor.sh` looks complete from a packet capture but
   misses `agentn.global.api5.cursor.sh`, which the agent loop uses and which is
   even stricter than the rest — over HTTP/1.1 it does not answer at all
   (`status=000`, connection refused) rather than returning an error status.
   Cursor routes across regional and per-service subdomains that change without
   notice, so any hand-written list is a latent outage.
2. **Merge** `onecli-cli` PR #112 (GUI sandbox launch, cursor-agent support).
3. **Trust the gateway CA** on the demo machine, Chromium ignores the env-var CA:
   ```
   # Delete first: a CA trusted for an EARLIER gateway has the same name but a
   # different key, and adding a second one does not fix it.
   security delete-certificate -c "OneCLI Local Gateway CA" 2>/dev/null

   # NOTE: no -d. That selects the ADMIN trust domain, which needs root; run as
   # a normal user it adds the certificate and applies NO trust settings, so the
   # cert looks installed and every TLS handshake still fails.
   security add-trusted-cert -r trustRoot \
     -k ~/Library/Keychains/login.keychain-db ~/.onecli/gateway-ca.pem
   ```
   `~/.onecli/gateway-ca.pem` is refreshed on every authenticated run, so it is
   always the CA the current gateway uses.

   Confirm trust was actually applied (presence is not trust):
   ```
   security dump-trust-settings | grep "OneCLI Local Gateway CA"
   ```
   No output means it is installed but untrusted, and GUI editors will still fail.

   **Verify it took** — this is the single highest-value pre-demo check, because
   a stale CA fails in a way that looks like every name-based check passing:
   ```
   onecli run --enforce -- cursor
   ```
   If the trusted CA is not the live one, OneCLI now prints a warning naming the
   exact commands to fix it. If you see `net::ERR_CERT_AUTHORITY_INVALID` in
   `~/Library/Application Support/Cursor/logs/<newest>/main.log`, that is this
   problem: the CA rotated after you trusted it. `security find-certificate` and
   `dump-trust-settings` will both still look correct, since only the key differs.
4. **Refresh vault credentials** for any agent you'll demo (Codex/Cursor tokens
   expire; a dead token looks like a product failure on stage).
5. **Quit Cursor** — macOS focuses an existing window instead of launching a
   sandboxed one, and OneCLI will (correctly) refuse.

---

## Demo 1 — the terminal agent (strongest, fully unbypassable)

```bash
onecli run --enforce -- cursor-agent
```

Then inside it, ask the agent to run:
```bash
curl -sS https://api.github.com/zen                    # works — via the gateway
curl --noproxy '*' --max-time 5 http://1.1.1.1/        # REFUSED by the OS in ms
```

**The point:** the second command fails in *milliseconds* (connect refused), not
by timeout. The agent cannot opt out — it has no route to the internet except the
gateway.

---

## Demo 2 — the IDE itself

```bash
onecli run --enforce -- cursor
```

Cursor opens **inside the sandbox**. Show, in this order:

1. **Its own traffic is governed.** In a second terminal:
   ```bash
   lsof -nP -iTCP:<forwarder-port>        # port is printed by onecli on launch
   ```
   You'll see the editor's live connections bridged out to the gateway.
2. **Use the AI chat.** (Requires step 1 of "Before the demo".)
3. **Try to escape** from Cursor's integrated terminal:
   ```bash
   curl --noproxy '*' --max-time 5 http://1.1.1.1/     # refused by the OS
   ```
4. **Show the dashboard** — app.onecli.sh → Activity — with the requests listed.

---

## Demo 3 — the red-team proof (do this one, it lands)

```bash
onecli sandbox audit
```

Runs every known escape technique against the real profile and reports whether
the OS actually stopped it: direct dial, proxy-env stripped, raw sockets, child
processes, IPv6, UDP/DNS exfil, LaunchServices `open`, AppleEvents, the Docker
socket, and the deferred-egress vectors (planting a LaunchAgent, writing
`.zshrc`, dropping a binary on `$PATH`, rewriting `~/.onecli`, reading SSH keys).

Expected: **PASS — no bypasses found**, plus the legitimate-capability checks
still working (DNS, git, home-cache writes), because a sandbox that breaks real
work gets switched off.

---

## What to say honestly if asked

- **Tunnelled hosts** (`GATEWAY_TUNNEL_HOSTS`) are the one real caveat, so state
  it plainly. They keep the two things that matter most for the demo's claim:
  egress is still confined to the gateway (the sandbox gives the agent no other
  route out), and the connection still requires a valid agent token, so it is
  attributed to a named agent/project/org. What they lose is everything that
  depends on reading the stream: credential injection, content inspection,
  per-request policy rules and rate limits, and per-request `request_logs` rows.
  Verified directly: an invalid token on a tunnelled host is still rejected 407.
  Scope the list to one vendor's domain (`*.cursor.sh` matches only subdomains of
  `cursor.sh`, never `evilcursor.sh`); never tunnel a domain you don't control the
  reason for. Full HTTP/2 MITM support is the planned fix that removes the caveat.
- **Enforcement covers agents OneCLI launches.** A developer who runs an agent
  outside OneCLI isn't sandboxed — that's what the enrollment/attestation layer
  on the roadmap addresses (PATH shims, coverage reporting).
- **Interactive OAuth logins** (`cursor-agent login`) need a browser, which
  enforce blocks by design. Authenticate once outside enforce, then enforce.

---

## Known-good vs blocked (as of last verification)

| Check | Status |
|---|---|
| `onecli sandbox audit` | ✅ PASS, all vectors refused |
| enforce-wrapped bash → gateway | ✅ 200 |
| bypass attempt from inside the wrap | ✅ refused by OS in 17ms |
| `cursor-agent` recognized + wrapped | ✅ |
| Cursor GUI launches sandboxed | ✅ 0 errors, 10 live gateway connections |
| Cursor in-IDE AI chat | ⏳ needs PR #754 deployed + `GATEWAY_TUNNEL_HOSTS` |
