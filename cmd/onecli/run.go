package main

import (
	"bytes"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/output"

	"gopkg.in/yaml.v3"
)

//go:embed skill_gateway_fallback.md
var gatewaySkillFallback string

//go:embed hook_gateway_detect.sh
var gatewayDetectHook string

//go:embed plugin_gateway_hermes.yaml
var hermesPluginManifest string

//go:embed plugin_gateway_hermes.py
var hermesPluginHandler string

//go:embed sitecustomize_onecli_ca.py
var caShimSource string

// RunCmd is `onecli run -- <command> [args...]`.
type RunCmd struct {
	Project string   `optional:"" short:"p" help:"Project slug."`
	Agent   string   `optional:"" name:"agent" help:"OneCLI agent identifier (default: ONECLI_AGENT env, then 'onecli config set agent', then the project's default agent)."`
	Gateway string   `optional:"" name:"gateway" help:"Gateway host:port override (default: derived from API host)."`
	NoCA    bool     `optional:"" name:"no-ca" help:"Skip writing the CA cert and CA trust env injection."`
	Enforce bool     `optional:"" name:"enforce" help:"OS-enforced governance: sandbox the agent so all egress is routed through the gateway and cannot be bypassed."`
	DryRun  bool     `optional:"" name:"dry-run" help:"Print resolved env and command without executing."`
	Args    []string `arg:"" optional:"" name:"command" help:"Command and arguments to execute (after --)."`
}

func (c *RunCmd) Run(out *output.Writer) error {
	if len(c.Args) == 0 {
		return fmt.Errorf("no command specified: use 'onecli run -- <command> [args...]'")
	}

	// Resolve the agent identity: --agent flag > ONECLI_AGENT env >
	// `onecli config set agent ...` (the machine-local pin); empty means the
	// project's server-side default agent. Validated at the boundary.
	agent, err := resolveAgent(c.Agent)
	if err != nil {
		return err
	}

	// Resolve the binary path early — fail fast before the API round-trip.
	binary, err := exec.LookPath(c.Args[0])
	if err != nil {
		return fmt.Errorf("command not found: %s — is it installed and in your PATH?", c.Args[0])
	}

	// Fetch gateway configuration from the API.
	client, err := newClient()
	if err != nil {
		return err
	}
	cfg, err := client.GetContainerConfig(newContext(), agent)
	if err != nil {
		return err
	}

	// Rewrite proxy URLs for local use. The server returns Docker-internal
	// hostnames (e.g. host.docker.internal) that don't resolve on the host
	// machine. Replace with the gateway host reachable from this machine.
	gatewayHost := c.Gateway
	if gatewayHost == "" {
		gatewayHost = resolveLocalGatewayHost()
	}

	// Derive the proxy URL Hermes' Docker sandbox should use, captured before
	// rewriteProxyEnvHosts mutates cfg.Env. The sandbox reaches the gateway at
	// the same host this process resolves it to — except a loopback host, which
	// a container can't reach and must hit via host.docker.internal.
	containerProxyURL := containerProxyURLFor(firstProxyURL(cfg.Env), gatewayHost)

	rewriteProxyEnvHosts(cfg.Env, gatewayHost)

	// The gateway proxy injects the API key at the HTTP level (x-api-key header).
	// Keeping it in the env triggers a first-run confirmation prompt in Claude Code.
	delete(cfg.Env, "ANTHROPIC_API_KEY")

	// Some agents read their home from an env var the server returns as a
	// container path (e.g. CODEX_HOME=/home/node/.codex), which doesn't exist
	// on the host — Codex aborts with "path does not exist". Rewrite these to
	// the local equivalent under the user's home, where onecli writes the auth
	// stub and native proxy config below.
	if home, err := os.UserHomeDir(); err == nil {
		rewriteContainerHomeEnv(cfg.Env, home)
	}

	// Dry-run: print resolved config without side effects (no CA write,
	// no skill install, no exec).
	if c.DryRun {
		injected := make([]string, 0, len(cfg.Env)+len(caTrustKeys))
		for k := range cfg.Env {
			injected = append(injected, k)
		}
		if !c.NoCA && cfg.CACertificate != "" {
			injected = append(injected, caTrustKeys...)
		}
		return out.WriteDryRun("Would exec command with OneCLI gateway", map[string]any{
			"binary":       binary,
			"args":         c.Args,
			"env_injected": injected,
		})
	}

	// Write CA cert to disk (unless --no-ca).
	caPath := ""
	if !c.NoCA && cfg.CACertificate != "" {
		caPath, err = writeGatewayCACert(cfg.CACertificate)
		if err != nil {
			// Non-fatal: warn and skip CA injection rather than aborting.
			out.Stderr(fmt.Sprintf("onecli: warning: could not write CA cert (%v); continuing without CA trust injection", err))
			caPath = ""
		}
	}

	// Enforce mode routes one of two ways. Agents with a cooperating
	// OS sandbox (Claude Code) keep the native integration: their own
	// sandbox becomes the enforcement layer via --settings. Everything
	// else gets the OneCLI-owned sandbox wrap (run_enforce_wrap.go).
	// The wrap decision must happen HERE — before the child env and any
	// agent config injections are derived from cfg.Env — because the
	// wrapped process can only dial loopback, so every proxy URL has to
	// be repointed at the forwarder first. Both paths fail closed.
	enforceNative := false
	wrapProfilePath := ""
	guiBinary := "" // non-empty when enforcing a GUI editor: exec this app-bundle binary instead of the CLI launcher
	var wrapPort uint16
	// Owns the pf anchor when transparent redirect is active; nil otherwise.
	var transparentSess *transparentSession
	if c.Enforce {
		spec, known := agentSkillDir(c.Args[0])
		switch {
		case known && enforceSupportedAgents[spec.agentName]:
			enforceNative = true
		case known && spec.dockerSandbox:
			// Tools run in a Docker container OUTSIDE any host sandbox
			// (and the wrap denies docker.sock as an egress bypass), so
			// the wrap cannot govern this agent. Fail closed.
			return fmt.Errorf("--enforce is not supported for %s: its tools run in a Docker sandbox the OS sandbox cannot govern", spec.agentName)
		case known && spec.configDir != "":
			// GUI editors (Cursor and other VS Code-style apps): the `cursor`
			// CLI is only a launcher — it hands off to an already-running
			// Electron app, so sandboxing IT would govern nothing. Instead we
			// exec the app bundle's own binary under the sandbox, which puts
			// the whole editor (AI calls, telemetry, extensions, its terminal)
			// inside the profile. Verified: Cursor starts normally and its own
			// update check to api2.cursor.sh is refused by the OS
			// (net::ERR_ACCESS_DENIED), i.e. enforcement the app cannot ignore.
			guiBinary, err = resolveGUIAppBinary(spec)
			if err != nil {
				return fmt.Errorf("--enforce for %s: %w", spec.agentName, err)
			}
			if running, pid := guiAlreadyRunning(spec); running {
				// macOS activates the EXISTING instance instead of starting
				// ours, so launching now would report enforcement while the
				// user keeps typing in whatever window is already open —
				// which may well be an ungoverned one. We can't tell from
				// here whether that instance is sandboxed, so refuse and let
				// the user restart it deliberately.
				return fmt.Errorf("--enforce for %s: it is already running (pid %d). Quit it first, then re-run: macOS would otherwise just focus the existing window instead of launching a sandboxed one", spec.agentName, pid)
			}
			wrapProfilePath, wrapPort, transparentSess, err = resolveEnforceWrapMode(cfg.Env)
			if err != nil {
				return fmt.Errorf("enforce mode unavailable: %w", err)
			}
		default:
			wrapProfilePath, wrapPort, transparentSess, err = resolveEnforceWrapMode(cfg.Env)
			if err != nil {
				return fmt.Errorf("enforce mode unavailable: %w", err)
			}
		}
		// The pf anchor must not outlive the run: a stale one keeps
		// redirecting a group whose listener is gone. The session also
		// installs signal handlers for the paths this defer cannot cover.
		if transparentSess != nil {
			defer func() { _ = transparentSess.Close() }()
		}
	}

	// Build child environment.
	env := buildChildEnv(os.Environ(), cfg.Env, caPath)

	env = append(env, "ONECLI_GATEWAY=true")

	// When the gateway routes Node's own egress via NODE_USE_ENV_PROXY, Node
	// prints a one-time "[UNDICI-EHPA] EnvHttpProxyAgent is experimental"
	// warning to stderr on startup — the mechanism announcing itself, not an
	// error. Mute just that warning code (Node 22+ --disable-warning) so
	// gateway plumbing doesn't leak noise into the agent's output. Scoped to
	// the flag's presence so we never alter Node behavior otherwise.
	env = suppressUndiciProxyWarning(env)

	// For known agents, fetch the agent-specific skill variant and install
	// to the agent's skill directory. Also optionally register a hook.
	agentFramework := strings.ToLower(filepath.Base(c.Args[0]))
	if a, ok := agentSkillDir(c.Args[0]); ok {
		skillContent := gatewaySkillFallback
		if fetched, err := client.GetGatewaySkill(newContext(), agentFramework); err == nil && fetched != "" {
			skillContent = fetched
		}
		maybeInstallGatewaySkill(out, a.agentName, a.baseDir, skillContent)
		if !a.skipHook {
			maybeInstallGatewayHook(out, a.agentName, a.baseDir, a.hooksFile)
		}
		if a.pluginGateway {
			maybeInstallGatewayPlugin(out, a.agentName, a.baseDir)
		}

		// Agents that refuse to start without a provider key (e.g. OpenClaw)
		// get a placeholder — the gateway swaps in the real key per request.
		// A key already in the user's shell wins (it passes through
		// buildChildEnv today for every agent, and the gateway replaces it on
		// the wire either way).
		if a.needsAnthropicKey {
			env = ensureEnv(env, "ANTHROPIC_API_KEY", anthropicKeyPlaceholder)
		}

		// Electron-based agents (e.g. Cursor) ignore embedded user:pass in
		// HTTPS_PROXY and show a native auth dialog. Inject proxy credentials
		// into the app's VS Code-style settings.json instead.
		//
		// NOT under --enforce: there we launch the app ourselves and pass
		// Chromium a native --proxy-server pointing at the loopback
		// forwarder, which injects the gateway credentials. The app-level
		// keys are then both redundant and actively harmful — Electron's
		// SimpleURLLoader rejects http.proxyAuthorization with
		// net::ERR_INVALID_ARGUMENT, failing every request the editor makes
		// (verified: removing the keys took the failures from 4 per launch
		// to 0, and the update check completed through the gateway).
		if a.configDir != "" {
			if guiBinary != "" {
				clearElectronProxySettings(out, a.configDir)
				// Chromium reads the OS keychain, not our CA env vars, so a
				// GUI editor is the one surface where an untrusted or rotated
				// CA silently breaks every request. Check before launching:
				// the symptom (ERR_CERT_AUTHORITY_INVALID with a correctly
				// named CA installed) is very hard to diagnose after the fact.
				warnIfGatewayCANotTrusted(out, cfg.CACertificate)
			} else {
				env = injectElectronProxySettings(out, env, a.configDir, caPath)
			}
		}

		// Agents with a native proxy config (e.g. Codex) need proxy_url
		// written to their TOML config and CODEX_CA_CERTIFICATE set.
		if a.nativeProxyConfig != "" {
			maybeInjectNativeProxyConfig(out, a.agentName, a.nativeProxyConfig, env, caPath)
		}
		if agentFramework == "codex" {
			maybeCreateCodexAuthStub(out, client)
		}

		// Agents that run tools in a Docker sandbox (e.g. Hermes) don't inherit
		// this process's proxy/CA env. Configure the sandbox via Hermes'
		// TERMINAL_DOCKER_* env overrides, make the gateway CA trusted by
		// certifi-pinned Python clients (httplib2) via a sitecustomize shim,
		// and route — and thereby govern — the agent's own inference traffic.
		if a.dockerSandbox {
			env = applyHermesGateway(out, env, a.baseDir, caPath, containerProxyURL)
		}
	} else {
		// Unknown agent — install the skill to ~/.onecli/skills/ so the
		// framework can discover it via ONECLI_GATEWAY_SKILL_PATH.
		skillContent := gatewaySkillFallback
		if fetched, err := client.GetGatewaySkill(newContext(), agentFramework); err == nil && fetched != "" {
			skillContent = fetched
		}
		if p := installUniversalGatewaySkill(out, skillContent); p != "" {
			env = append(env, "ONECLI_GATEWAY_SKILL_PATH="+p)
		}
	}

	// Surface any warnings from the server (e.g. missing credentials).
	for _, w := range cfg.Warnings {
		out.Stderr(fmt.Sprintf("onecli: warning: %s", w))
	}

	// Native enforce path: fork the loopback auth forwarder, write the
	// sandbox settings, and extend the agent argv. Fails closed — a
	// broken forwarder would leave the sandbox with no route to the
	// gateway, which is worse than an explicit error.
	args := c.Args
	if enforceNative {
		port, err := spawnEnforceForwarder(firstProxyURL(cfg.Env))
		if err != nil {
			return fmt.Errorf("enforce mode unavailable: %w", err)
		}
		settingsPath, err := writeEnforceSettings(port)
		if err != nil {
			return fmt.Errorf("enforce mode unavailable: %w", err)
		}
		args = append(append([]string{}, c.Args...), enforceAgentArgs(settingsPath)...)
		out.Stderr(fmt.Sprintf("onecli: enforce mode active — sandboxed egress locked to the gateway (forwarder :%d).", port))
	}

	// Wrap enforce path: exec sandbox-exec around the agent so the OS
	// confines the whole process tree to loopback-only egress.
	execBinary := binary
	if wrapProfilePath != "" {
		execBinary, err = enforceWrapLauncher()
		if err != nil {
			return fmt.Errorf("enforce mode unavailable: %w", err)
		}
		// For a GUI editor, confine the app bundle's own binary rather than
		// the CLI launcher (which would exit immediately, leaving the real
		// editor running outside the sandbox). Its own args are dropped:
		// the launcher's argv (e.g. a path to open) doesn't apply to the
		// Electron entrypoint.
		sandboxed, sandboxedArgs := binary, c.Args[1:]
		if guiBinary != "" {
			sandboxed, sandboxedArgs = guiBinary, nil
		}
		args = enforceWrapArgv(wrapProfilePath, sandboxed, sandboxedArgs, agentFramework, wrapPort)
		// Transparent redirect scopes pf by GID, so the confined tree must
		// adopt the sandbox group. The helper wraps the LAUNCHER, not the
		// other way round: Seatbelt refuses to exec a setgid binary from
		// inside the sandbox (verified: execvp Operation not permitted).
		if transparentSess != nil {
			args = transparentWrapArgv(args)
			execBinary = setgidHelperPath
		}
		if notice := enforceWrapNotice(agentFramework); notice != "" {
			out.Stderr(notice)
		}
		out.Stderr(fmt.Sprintf("onecli: enforce mode active — all process egress locked to the gateway (forwarder :%d).", wrapPort))
		if guiBinary != "" {
			out.Stderr(fmt.Sprintf("onecli: launching %s inside the sandbox; close it from the app, not this terminal.", filepath.Base(guiBinary)))
		}
	}

	// Exec — replaces this process so the agent gets direct terminal control.
	out.Stderr(fmt.Sprintf("onecli: gateway connected. Starting %s...", c.Args[0]))
	if err := syscall.Exec(execBinary, args, env); err != nil {
		return fmt.Errorf("could not start %s: %w", c.Args[0], err)
	}
	return nil
}

// writeGatewayCACert writes a combined CA bundle (system CAs + gateway CA)
// to ~/.onecli/ca-bundle.pem. Env vars like SSL_CERT_FILE REPLACE the
// default trust store, so the bundle must include system root certificates
// alongside the gateway CA.
func writeGatewayCACert(gatewayPEM string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	caPath := filepath.Join(home, ".onecli", "ca-bundle.pem")
	if err := os.MkdirAll(filepath.Dir(caPath), 0o700); err != nil {
		return "", fmt.Errorf("creating CA dir: %w", err)
	}

	var buf bytes.Buffer
	if systemCAs, err := readSystemCAs(); err == nil {
		buf.Write(systemCAs)
		if len(systemCAs) > 0 && systemCAs[len(systemCAs)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	buf.WriteString(gatewayPEM)

	combined := buf.Bytes()

	// Also keep the bare gateway CA on disk, always in step with the bundle.
	// Tools that need a CA they can *install* rather than point an env var at
	// (Chromium/Electron reads the OS keychain and ignores SSL_CERT_FILE, so
	// governing Cursor's GUI requires `security add-trusted-cert`) need the
	// single certificate, not the ~200-root bundle. Writing it here rather
	// than on first use is deliberate: a copy that is refreshed on some other
	// schedule than the bundle goes stale silently, and a stale CA fails as
	// ERR_CERT_AUTHORITY_INVALID with a *correct-looking* subject name, which
	// is a genuinely hard error to read. Observed in practice: a months-old
	// gateway-ca.pem was trusted in the keychain while the gateway had since
	// rotated, so every Chromium request failed even though `security
	// find-certificate` showed "OneCLI Local Gateway CA" present and trusted.
	// Best-effort: the bundle is what enforce actually depends on, and the
	// bare copy only matters for the manual keychain-install step. A failure
	// here is surfaced later by warnIfGatewayCANotTrusted, which compares
	// against the live CA rather than this file.
	_ = writeBareGatewayCA(filepath.Dir(caPath), gatewayPEM)

	existing, err := os.ReadFile(caPath)
	if err == nil && bytes.Equal(existing, combined) {
		return caPath, nil
	}
	if err := os.WriteFile(caPath, combined, 0o600); err != nil {
		return "", fmt.Errorf("writing CA bundle: %w", err)
	}
	return caPath, nil
}

// writeBareGatewayCA writes just the gateway CA to <dir>/gateway-ca.pem.
// Separate from the bundle so it can be installed into an OS trust store.
func writeBareGatewayCA(dir, gatewayPEM string) error {
	if strings.TrimSpace(gatewayPEM) == "" {
		return nil
	}
	path := filepath.Join(dir, "gateway-ca.pem")
	pem := []byte(gatewayPEM)
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, pem) {
		return nil
	}
	return os.WriteFile(path, pem, 0o600)
}

var systemCAPaths = []string{
	"/etc/ssl/cert.pem",                  // macOS
	"/etc/ssl/certs/ca-certificates.crt", // Debian/Ubuntu
	"/etc/pki/tls/certs/ca-bundle.crt",   // RHEL/Fedora/CentOS
	"/etc/ssl/ca-bundle.pem",             // SUSE
}

func readSystemCAs() ([]byte, error) {
	for _, p := range systemCAPaths {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("no system CA bundle found")
}

// caTrustKeys are env vars we inject locally for CA trust. These aren't in
// the server response but may exist in the parent env and need stripping.
var caTrustKeys = []string{
	"NODE_EXTRA_CA_CERTS",
	"SSL_CERT_FILE",
	"REQUESTS_CA_BUNDLE",
	"AWS_CA_BUNDLE",
	"CURL_CA_BUNDLE",
	"GIT_SSL_CAINFO",
	"DENO_CERT",
}

// buildChildEnv builds the environment for the child process by stripping
// conflicting keys from the current env, appending the server-provided env,
// and overriding CA cert paths to use the local file.
func buildChildEnv(current []string, serverEnv map[string]string, caPath string) []string {
	// Strip keys the server provides + CA trust keys we inject locally.
	// This prevents stale inherited values (e.g. a corporate HTTPS_PROXY)
	// from shadowing the gateway values — POSIX getenv returns the first match.
	stripKeys := make(map[string]struct{}, len(serverEnv)+len(caTrustKeys))
	for k := range serverEnv {
		stripKeys[k] = struct{}{}
	}
	for _, k := range caTrustKeys {
		stripKeys[k] = struct{}{}
	}

	out := make([]string, 0, len(current)+len(serverEnv)+6)
	for _, kv := range current {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			out = append(out, kv)
			continue
		}
		if _, drop := stripKeys[kv[:i]]; drop {
			continue
		}
		out = append(out, kv)
	}

	// Build set of CA trust keys we'll override locally — skip these from
	// serverEnv so the local paths (appended below) aren't shadowed.
	// POSIX getenv returns the first match, so order matters.
	localCAKeys := make(map[string]struct{}, len(caTrustKeys))
	if caPath != "" {
		for _, k := range caTrustKeys {
			localCAKeys[k] = struct{}{}
		}
	}

	// Append server-provided env (HTTPS_PROXY, credentials, etc.),
	// excluding any CA trust keys we'll override with local paths.
	for k, v := range serverEnv {
		if _, skip := localCAKeys[k]; skip {
			continue
		}
		out = append(out, k+"="+v)
	}

	// Append CA trust vars pointing to the local cert file, replacing the
	// Docker container path that the server returns in NODE_EXTRA_CA_CERTS.
	if caPath != "" {
		out = append(out,
			"NODE_EXTRA_CA_CERTS="+caPath,
			"SSL_CERT_FILE="+caPath,
			"REQUESTS_CA_BUNDLE="+caPath,
			"CURL_CA_BUNDLE="+caPath,
			"GIT_SSL_CAINFO="+caPath,
			"DENO_CERT="+caPath,
		)
	}

	return out
}

// undiciWarningFlag mutes Node's experimental EnvHttpProxyAgent warning by its
// stable code (Node 22+). Scoped to a single code so real warnings still show.
const undiciWarningFlag = "--disable-warning=UNDICI-EHPA"

// suppressUndiciProxyWarning appends undiciWarningFlag to NODE_OPTIONS, but only
// when the gateway has enabled Node's env-proxy support (NODE_USE_ENV_PROXY) —
// the setting that triggers the warning. It edits an existing NODE_OPTIONS in
// place (preserving the user's flags) or appends a new one, and is a no-op if
// the flag is already present. POSIX getenv returns the first match, so editing
// in place rather than appending a second NODE_OPTIONS matters.
func suppressUndiciProxyWarning(env []string) []string {
	hasProxyFlag := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "NODE_USE_ENV_PROXY=") {
			hasProxyFlag = true
			break
		}
	}
	if !hasProxyFlag {
		return env
	}
	const key = "NODE_OPTIONS="
	for i, kv := range env {
		if !strings.HasPrefix(kv, key) {
			continue
		}
		existing := kv[len(key):]
		if strings.Contains(existing, undiciWarningFlag) {
			return env
		}
		if existing == "" {
			env[i] = key + undiciWarningFlag
		} else {
			env[i] = key + existing + " " + undiciWarningFlag
		}
		return env
	}
	return append(env, key+undiciWarningFlag)
}

// proxyEnvKeys are the proxy URL env vars (both casings) the gateway sets.
var proxyEnvKeys = []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"}

// dockerInternalHosts is the set of hostnames used inside Docker containers to
// reach the host machine. These don't resolve from a local process.
var dockerInternalHosts = map[string]bool{
	"host.docker.internal":    true,
	"gateway.docker.internal": true,
}

// resolveLocalGatewayHost derives the gateway hostname from the API host the
// CLI is configured to talk to. If the API host is localhost/127.0.0.1, the
// gateway is on the same machine. For remote hosts, use the same hostname
// (the gateway is typically co-located with the web app).
func resolveLocalGatewayHost() string {
	apiHost := config.APIHost()
	u, err := url.Parse(apiHost)
	if err != nil || u.Hostname() == "" {
		return "127.0.0.1"
	}
	return u.Hostname()
}

// containerHomeEnv maps env vars that the server returns as container-internal
// home paths to their home-relative local equivalent. A local agent process
// needs host paths (where onecli writes the agent's auth stub and config), not
// the Docker sandbox paths the server returns (e.g. CODEX_HOME=/home/node/.codex).
var containerHomeEnv = map[string]string{
	"CODEX_HOME": ".codex",
}

// rewriteContainerHomeEnv replaces container-internal home paths in the server
// env with the local equivalent under home. Codex aborts when CODEX_HOME points
// at a path that does not exist on the host, so the container path must be
// translated before exec. Mutating cfg.Env (rather than only appending later)
// also ensures buildChildEnv strips any stale inherited value, so the container
// path can't shadow the rewritten one.
func rewriteContainerHomeEnv(env map[string]string, home string) {
	if home == "" {
		return
	}
	for k, rel := range containerHomeEnv {
		if _, ok := env[k]; ok {
			env[k] = filepath.Join(home, rel)
		}
	}
}

// rewriteProxyEnvHosts replaces Docker-internal hostnames in proxy URL values
// with the given local host, keeping the port and credentials intact.
// Only rewrites values that look like proxy URLs (contain "://").
func rewriteProxyEnvHosts(env map[string]string, localHost string) {
	for k, v := range env {
		if !slices.Contains(proxyEnvKeys, k) {
			continue
		}
		u, err := url.Parse(v)
		if err != nil || !dockerInternalHosts[u.Hostname()] {
			continue
		}
		env[k] = proxyURLWithHost(v, localHost)
	}
}

// isLoopbackHost reports whether h is a loopback host a Docker container cannot
// reach directly (so it must go through host.docker.internal instead).
func isLoopbackHost(h string) bool {
	switch strings.ToLower(h) {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}

// containerProxyURLFor returns the proxy URL Hermes' Docker sandbox should use
// to reach the gateway. The gateway lives at gatewayHost (where this process
// reaches it): a container reaches a routable host directly, but a loopback
// host must be reached via host.docker.internal (paired with --add-host on
// Linux). serverProxy supplies the scheme, credentials, and port.
func containerProxyURLFor(serverProxy, gatewayHost string) string {
	host := gatewayHost
	if isLoopbackHost(host) {
		host = "host.docker.internal"
	}
	return proxyURLWithHost(serverProxy, host)
}

// agentSpec describes how `onecli run` integrates a known coding agent with the
// gateway: where its skill/hook/plugin files live and which injection
// strategies it needs.
type agentSpec struct {
	agentName         string
	baseDir           string // home-relative config dir (skills/hooks/plugins live here)
	configDir         string // VS Code-style app dir name; non-empty enables Electron proxy-settings injection.
	appBundle         string // macOS .app bundle name for GUI editors; under --enforce we exec its binary inside the sandbox instead of the CLI launcher (which only focuses a running app).
	skipHook          bool   // true when the gateway hook shouldn't be registered — either the agent has no Claude Code-style hooks (Hermes), or it renders injected hook context visibly in the transcript (Codex), where the auto-loaded onecli-gateway skill carries the same guidance without the noise.
	pluginGateway     bool   // true for agents that load the transform_tool_result recovery plugin (e.g. Hermes).
	dockerSandbox     bool   // true for agents that run tools in a Docker sandbox needing TERMINAL_DOCKER_* injection.
	needsAnthropicKey bool   // true for agents that refuse to start without a provider key in the env (e.g. OpenClaw); a placeholder is ensured, the gateway swaps in the real key per request.
	nativeProxyConfig string // home-relative dir with a TOML config needing proxy_url injection (e.g. ".codex").
	hooksFile         string // home-relative hook registration file; empty means Claude Code-style <baseDir>/settings.json.
}

// supportedAgents maps CLI binary base-names to their gateway integration spec.
var supportedAgents = []struct {
	bases []string
	spec  agentSpec
}{
	{[]string{"claude"}, agentSpec{agentName: "Claude Code", baseDir: ".claude"}},
	// Cursor has two surfaces with OPPOSITE enforce behavior:
	//   - the GUI launcher (`cursor`, or `agent` when invoked that way)
	//     only opens/focuses the Electron IDE — no launched process tree to
	//     sandbox — so configDir routes it to cooperative proxy-settings
	//     injection and fails --enforce closed (see the enforce switch).
	//   - the headless agent (`cursor-agent`) IS a launched CLI process,
	//     exactly like Codex/Claude, so it OMITS configDir and gets the
	//     real OS-enforced wrap under --enforce. Keeping them as separate
	//     specs is what lets the same "Cursor" cover both correctly.
	{[]string{"cursor", "agent"}, agentSpec{agentName: "Cursor", baseDir: ".cursor", configDir: "Cursor", appBundle: "Cursor"}},
	{[]string{"cursor-agent"}, agentSpec{agentName: "Cursor Agent", baseDir: ".cursor"}},
	// Codex skips the hook: it echoes injected hook context into the
	// transcript (Claude injects it silently), so the hook is pure noise
	// there. The onecli-gateway skill installed above auto-loads under the
	// gateway and carries the same guidance.
	{[]string{"codex"}, agentSpec{agentName: "Codex", baseDir: ".agents", skipHook: true, nativeProxyConfig: ".codex"}},
	{[]string{"hermes"}, agentSpec{agentName: "Hermes", baseDir: ".hermes", skipHook: true, pluginGateway: true, dockerSandbox: true}},
	{[]string{"opencode"}, agentSpec{agentName: "OpenCode", baseDir: ".opencode"}},
	// OpenClaw loads skills from ~/.openclaw/skills; its hook system is its
	// own (not Claude-style settings.json), so the hook install is skipped.
	// Its long-lived process is `openclaw gateway run`, and it honors the
	// injected proxy env via undici's EnvHttpProxyAgent.
	{[]string{"openclaw"}, agentSpec{agentName: "OpenClaw", baseDir: ".openclaw", skipHook: true, needsAnthropicKey: true}},
}

// anthropicKeyPlaceholder satisfies agents that refuse to start without a
// provider key in their environment (needsAnthropicKey). It is never a real
// credential: the gateway replaces x-api-key on every injected request, so
// this value exists only to pass the agent's local boot check — and is
// self-describing if it ever surfaces in an upstream 401.
const anthropicKeyPlaceholder = "sk-ant-onecli-gateway-placeholder"

// ensureEnv appends key=value when key is absent. An existing entry wins —
// POSIX getenv returns the first match, and a user's own shell value (which
// buildChildEnv deliberately passes through) must keep doing what it does
// today for every agent.
func ensureEnv(env []string, key, value string) []string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return env
		}
	}
	return append(env, prefix+value)
}

// agentSkillDir returns the integration spec for a known agent command, or
// ok=false if the command is not recognized.
func agentSkillDir(cmd string) (agentSpec, bool) {
	base := filepath.Base(cmd)
	for _, a := range supportedAgents {
		if slices.Contains(a.bases, base) {
			return a.spec, true
		}
	}
	return agentSpec{}, false
}

// maybeInstallGatewaySkill installs the OneCLI gateway skill file if it is
// missing or stale. agentName is used in user-facing messages.
func maybeInstallGatewaySkill(out *output.Writer, agentName, baseDir, content string) {
	home, err := os.UserHomeDir()
	if err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not resolve home directory: %v", err))
		return
	}
	fullPath := filepath.Join(home, baseDir, "skills", "onecli-gateway", "SKILL.md")

	existing, err := os.ReadFile(fullPath)
	if err == nil && bytes.Equal(existing, []byte(content)) {
		return
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not create skill directory: %v", err))
		return
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not write skill file: %v", err))
		return
	}
	out.Stderr(fmt.Sprintf("onecli: installed gateway skill for %s.", agentName))
}

// installUniversalGatewaySkill writes the gateway skill to
// ~/.onecli/skills/gateway.md so any framework can reference it via
// the ONECLI_GATEWAY_SKILL_PATH env var. Returns the path on success.
func installUniversalGatewaySkill(out *output.Writer, content string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	fullPath := filepath.Join(home, ".onecli", "skills", "gateway.md")

	existing, err := os.ReadFile(fullPath)
	if err == nil && bytes.Equal(existing, []byte(content)) {
		return fullPath
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not create universal skill directory: %v", err))
		return ""
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not write universal skill file: %v", err))
		return ""
	}
	return fullPath
}

// codexAuthStub builds the auth.json stub written to ~/.codex/auth.json when the
// file does not exist. The id_token is a structurally valid JWT with email and
// plan_type claims so Codex's local validation passes. last_refresh is stamped
// with the current time so Codex does not treat the onecli-managed tokens as
// stale and try to self-refresh them; real credentials are injected at the
// gateway proxy level.
func codexAuthStub() string {
	return fmt.Sprintf(`{
  "auth_mode": "chatgpt",
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJvbmVjbGktbWFuYWdlZCIsImVtYWlsIjoib25lY2xpQG9uZWNsaS5zaCIsImV4cCI6NDEwMjQ0NDgwMCwiaWF0IjoxNzM1Njg5NjAwLCJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9wbGFuX3R5cGUiOiJmcmVlIiwiY2hhdGdwdF91c2VyX2lkIjoib25lY2xpLW1hbmFnZWQiLCJjaGF0Z3B0X2FjY291bnRfaWQiOiJvbmVjbGktbWFuYWdlZCJ9fQ.b25lY2xpLW1hbmFnZWQtc2lnbmF0dXJl",
    "access_token": "onecli-managed",
    "refresh_token": "onecli-managed",
    "account_id": "onecli-managed"
  },
  "last_refresh": %q
}
`, time.Now().UTC().Format(time.RFC3339))
}

// maybeCreateCodexAuthStub creates ~/.codex/auth.json with onecli-managed
// placeholder values if the file does not already exist. Fetches the latest
// stub from the API; falls back to the embedded constant.
func maybeCreateCodexAuthStub(out *output.Writer, client *api.Client) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	authPath := filepath.Join(home, ".codex", "auth.json")
	if _, err := os.Stat(authPath); err == nil {
		return
	}

	content := codexAuthStub()
	if stub, err := client.GetCredentialStub(newContext(), "codex"); err == nil && stub.Content != "" {
		content = stub.Content
	}

	if err := os.MkdirAll(filepath.Dir(authPath), 0o750); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not create .codex directory: %v", err))
		return
	}
	if err := os.WriteFile(authPath, []byte(content), 0o600); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not write codex auth stub: %v", err))
		return
	}
	out.Stderr("onecli: created ~/.codex/auth.json stub for gateway auth.")
}

// maybeInjectNativeProxyConfig writes proxy_url into a TOML config file for
// agents that have their own managed proxy (e.g. Codex), refreshing a stale
// gateway-owned value from a previous run. Also sets the agent-specific CA
// certificate env var.
func maybeInjectNativeProxyConfig(out *output.Writer, agentName, configRelDir string, env []string, caPath string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	proxyURL := findProxyURL(env)
	if proxyURL == "" {
		return
	}

	configPath := filepath.Join(home, configRelDir, "config.toml")
	data, _ := os.ReadFile(configPath)

	updated, status := upsertGatewayProxyURL(string(data), proxyURL)
	switch status {
	case proxyURLCurrent:
		// Already pointing at this gateway URL; nothing to write.
	case proxyURLForeign:
		out.Stderr(fmt.Sprintf("onecli: warning: %s has a custom proxy_url in config.toml; leaving it unchanged, so native %s traffic will bypass the gateway.", agentName, agentName))
	default:
		if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
			out.Stderr(fmt.Sprintf("onecli: warning: could not write proxy config for %s: %v", agentName, err))
			return
		}
		if status == proxyURLAdded {
			out.Stderr(fmt.Sprintf("onecli: configured native proxy for %s.", agentName))
		} else {
			out.Stderr(fmt.Sprintf("onecli: updated native proxy for %s.", agentName))
		}
	}

	// Set CODEX_CA_CERTIFICATE if we have a CA path — Codex reads this
	// in addition to SSL_CERT_FILE for its Rust TLS client.
	if caPath != "" {
		os.Setenv("CODEX_CA_CERTIFICATE", caPath)
	}
}

type proxyURLStatus int

const (
	proxyURLAdded proxyURLStatus = iota
	proxyURLRefreshed
	proxyURLCurrent
	proxyURLForeign
)

var proxyURLLineRE = regexp.MustCompile(`^([ \t]*proxy_url[ \t]*=[ \t]*)"([^"]*)"[ \t]*$`)

// upsertGatewayProxyURL ensures content carries proxy_url = "<proxyURL>",
// appending a [network] section when the file has none. Gateway proxy URLs
// embed a per-run aoc_ session token, so a previously injected value goes
// stale on every new run; only values carrying that marker are rewritten
// (in place, preserving the rest of the file). A user-managed proxy_url —
// or one on a line we cannot parse, e.g. with a trailing comment — is left
// untouched.
func upsertGatewayProxyURL(content, proxyURL string) (string, proxyURLStatus) {
	if !strings.Contains(content, "proxy_url") {
		return content + "\n[network]\nproxy_url = \"" + proxyURL + "\"\n", proxyURLAdded
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		m := proxyURLLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[2] == proxyURL {
			return content, proxyURLCurrent
		}
		if !strings.Contains(m[2], "aoc_") {
			return content, proxyURLForeign
		}
		lines[i] = m[1] + `"` + proxyURL + `"`
		return strings.Join(lines, "\n"), proxyURLRefreshed
	}
	return content, proxyURLForeign
}

// maybeInstallGatewayPlugin installs the Hermes transform_tool_result recovery
// plugin and enables it in ~/.hermes/config.yaml. The plugin runs in the agent
// process and appends gateway recovery guidance to any tool result that looks
// like an auth error, so the agent creates a credential stub instead of
// following a manual OAuth/API-key setup flow.
func maybeInstallGatewayPlugin(out *output.Writer, agentName, baseDir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	pluginDir := filepath.Join(home, baseDir, "plugins", "onecli-gateway")

	wroteManifest := writeIfChanged(out, filepath.Join(pluginDir, "plugin.yaml"), hermesPluginManifest)
	wroteHandler := writeIfChanged(out, filepath.Join(pluginDir, "__init__.py"), hermesPluginHandler)
	if wroteManifest || wroteHandler {
		out.Stderr(fmt.Sprintf("onecli: installed gateway plugin for %s.", agentName))
	}

	// Plugins are opt-in: a plugin only loads if listed under plugins.enabled
	// in config.yaml. Edit the file via a YAML round-trip so other settings and
	// comments are preserved (no fragile string surgery).
	configPath := filepath.Join(home, baseDir, "config.yaml")
	if changed, err := enableHermesPlugin(configPath, "onecli-gateway"); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not enable gateway plugin: %v", err))
	} else if changed {
		out.Stderr(fmt.Sprintf("onecli: enabled gateway plugin in %s config.", agentName))
	}
}

// applyHermesGateway makes the gateway reach where Hermes actually sends
// traffic. Hermes runs its own LLM/inference on this host (httpx, which honors
// HTTPS_PROXY + SSL_CERT_FILE — already set by buildChildEnv), but runs *tools*
// in a separate Docker sandbox that inherits none of this process's env. It
// returns env extended with: (1) a CA-trust shim for certifi-pinned Python
// clients (httplib2 → Google Workspace), and (2) Hermes' TERMINAL_DOCKER_*
// overrides that push the proxy + CA + shim into the sandbox container (no
// config-file mutation; inert when terminal.backend != docker).
func applyHermesGateway(out *output.Writer, env []string, baseDir, caPath, containerProxyURL string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return env
	}
	cfg := readHermesConfig(filepath.Join(home, baseDir, "config.yaml"))

	// Host side: Hermes' inference (httpx) already trusts the gateway CA via
	// SSL_CERT_FILE. Add HERMES_CA_BUNDLE (Hermes' native CA knob) and a
	// sitecustomize shim so certifi-pinned clients (httplib2) trust it too —
	// this also covers Google Workspace when terminal.backend is "local".
	shimDir := ""
	if caPath != "" {
		env = append(env, "HERMES_CA_BUNDLE="+caPath, "ONECLI_CA_BUNDLE="+caPath)
		if shimDir = installCAShim(out); shimDir != "" {
			env = prependPythonPath(env, shimDir)
		}
	}

	// Inference governance: Hermes' model calls flow through the gateway, so
	// OneCLI sees and can police them. Make that visible.
	out.Stderr("onecli: Hermes inference is routed through the OneCLI gateway; " +
		"under a deny-by-default policy, allow your model-provider host in OneCLI rules.")

	// Sandbox side: route Hermes' Docker tool-sandbox through the gateway via
	// env-var overrides (merged with the user's config in hermesSandboxEnv).
	return append(env, hermesSandboxEnv(cfg, caPath, shimDir, containerProxyURL)...)
}

// hermesSandboxEnv returns the TERMINAL_DOCKER_* env overrides that route
// Hermes' Docker tool-sandbox through the gateway, merged with any docker_env /
// docker_volumes / docker_extra_args already in the user's config. It performs
// no I/O so it can be unit-tested. Disabling cross-process container reuse
// forces a fresh container that picks up the proxy + CA (Hermes reuses by label
// and ignores env/mount changes; on-disk filesystem persistence is unaffected).
func hermesSandboxEnv(cfg hermesConfig, caPath, shimDir, containerProxyURL string) []string {
	const containerCA = "/etc/ssl/onecli-ca.pem"
	const containerShim = "/opt/onecli-pyca"

	dockerEnv := map[string]string{"ONECLI_GATEWAY": "true"}
	for k, v := range cfg.Terminal.DockerEnv {
		dockerEnv[k] = fmt.Sprint(v)
	}
	if containerProxyURL != "" {
		for _, k := range proxyEnvKeys {
			dockerEnv[k] = containerProxyURL
		}
	}
	if caPath != "" {
		for _, k := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO", "ONECLI_CA_BUNDLE"} {
			dockerEnv[k] = containerCA
		}
	}
	// Prepend the CA shim to PYTHONPATH (container path separator is always ":"),
	// preserving any PYTHONPATH the user set in docker_env. Only when the shim is
	// actually mounted (shimDir != "") — otherwise the path wouldn't exist.
	if shimDir != "" {
		if existing := dockerEnv["PYTHONPATH"]; existing != "" {
			dockerEnv["PYTHONPATH"] = containerShim + ":" + existing
		} else {
			dockerEnv["PYTHONPATH"] = containerShim
		}
	}

	volumes := append([]string{}, cfg.Terminal.DockerVolumes...)
	if caPath != "" {
		if caVol := caPath + ":" + containerCA + ":ro"; !slices.Contains(volumes, caVol) {
			volumes = append(volumes, caVol)
		}
		if shimDir != "" {
			if shimVol := shimDir + ":" + containerShim + ":ro"; !slices.Contains(volumes, shimVol) {
				volumes = append(volumes, shimVol)
			}
		}
	}

	// --add-host is only needed when the sandbox reaches the gateway via
	// host.docker.internal (Linux doesn't resolve that name automatically).
	// For a routable gateway host the container connects directly, so skip it.
	extraArgs := append([]string{}, cfg.Terminal.DockerExtraArgs...)
	if runtime.GOOS == "linux" && proxyURLHostname(containerProxyURL) == "host.docker.internal" &&
		!slices.Contains(extraArgs, "host.docker.internal:host-gateway") {
		extraArgs = append(extraArgs, "--add-host", "host.docker.internal:host-gateway")
	}

	var out []string
	if b, err := json.Marshal(dockerEnv); err == nil {
		out = append(out, "TERMINAL_DOCKER_ENV="+string(b))
	}
	if b, err := json.Marshal(volumes); err == nil {
		out = append(out, "TERMINAL_DOCKER_VOLUMES="+string(b))
	}
	if b, err := json.Marshal(extraArgs); err == nil {
		out = append(out, "TERMINAL_DOCKER_EXTRA_ARGS="+string(b))
	}
	return append(out, "TERMINAL_DOCKER_PERSIST_ACROSS_PROCESSES=false")
}

// hermesConfig is the subset of ~/.hermes/config.yaml we read (best-effort) to
// merge sandbox settings without clobbering the user's.
type hermesConfig struct {
	Terminal struct {
		DockerEnv       map[string]any `yaml:"docker_env"`
		DockerVolumes   []string       `yaml:"docker_volumes"`
		DockerExtraArgs []string       `yaml:"docker_extra_args"`
	} `yaml:"terminal"`
}

func readHermesConfig(configPath string) hermesConfig {
	var cfg hermesConfig
	if data, err := os.ReadFile(configPath); err == nil {
		_ = yaml.Unmarshal(data, &cfg) // best-effort; absent keys stay zero
	}
	return cfg
}

// enableHermesPlugin adds name to plugins.enabled in a Hermes config.yaml,
// preserving the rest of the document (keys, order, comments) via a yaml.Node
// round-trip. Returns whether the file was changed.
func enableHermesPlugin(configPath, name string) (bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if os.IsNotExist(err) || len(bytes.TrimSpace(data)) == 0 {
		// Hermes deep-merges defaults at load, so a minimal file is sufficient.
		if err := os.MkdirAll(filepath.Dir(configPath), 0o750); err != nil {
			return false, err
		}
		content := "plugins:\n  enabled:\n    - " + name + "\n"
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			return false, err
		}
		return true, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("parsing config.yaml: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false, fmt.Errorf("unexpected config.yaml structure")
	}
	root := doc.Content[0]

	// Duplicate top-level keys are ambiguous: yaml.v3 keeps both, but Hermes'
	// loader is last-key-wins — editing the first block would silently fail to
	// enable the plugin. Refuse rather than report a false success.
	if yamlMapCount(root, "plugins") > 1 {
		return false, fmt.Errorf("config.yaml has duplicate top-level 'plugins' keys; enable onecli-gateway manually")
	}

	plugins := yamlMapGet(root, "plugins")
	if plugins == nil || plugins.Kind != yaml.MappingNode {
		plugins = &yaml.Node{Kind: yaml.MappingNode}
		yamlMapSet(root, "plugins", plugins)
	} else if yamlMapCount(plugins, "enabled") > 1 {
		return false, fmt.Errorf("config.yaml has duplicate 'plugins.enabled' keys; enable onecli-gateway manually")
	}

	enabled := yamlMapGet(plugins, "enabled")
	switch {
	case enabled == nil:
		enabled = &yaml.Node{Kind: yaml.SequenceNode}
		yamlMapSet(plugins, "enabled", enabled)
	case enabled.Kind == yaml.ScalarNode && enabled.Value != "" && enabled.Tag != "!!null":
		// Single-scalar form (`enabled: foo`): promote to a sequence, keeping
		// the user's existing value instead of dropping it. Explicit nulls
		// (`enabled: null` / `~`) are tagged !!null with a non-empty Value, so
		// they're excluded here and fall through to the fresh-sequence case.
		kept := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: enabled.Value}
		enabled = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{kept}}
		yamlMapSet(plugins, "enabled", enabled)
	case enabled.Kind != yaml.SequenceNode:
		// null / mapping / other — replace with a fresh sequence.
		enabled = &yaml.Node{Kind: yaml.SequenceNode}
		yamlMapSet(plugins, "enabled", enabled)
	}
	for _, item := range enabled.Content {
		if item.Value == name {
			return false, nil
		}
	}
	enabled.Content = append(enabled.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name})

	encoded, err := yaml.Marshal(&doc)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// yamlMapGet returns the value node for key in a YAML mapping node, or nil.
func yamlMapGet(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// yamlMapCount returns how many times key appears in a YAML mapping node.
func yamlMapCount(m *yaml.Node, key string) int {
	n := 0
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			n++
		}
	}
	return n
}

// yamlMapSet sets key=val in a YAML mapping node, appending if absent.
func yamlMapSet(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
}

// installCAShim writes the embedded sitecustomize CA shim to ~/.onecli/pyca/
// and returns that directory (mountable into the sandbox), or "" on failure.
func installCAShim(out *output.Writer) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".onecli", "pyca")
	path := filepath.Join(dir, "sitecustomize.py")
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, []byte(caShimSource)) {
		return dir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not create CA shim dir: %v", err))
		return ""
	}
	if err := os.WriteFile(path, []byte(caShimSource), 0o644); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not write CA shim: %v", err))
		return ""
	}
	return dir
}

// writeIfChanged writes content to path (creating parent dirs) unless the file
// already holds exactly content. Returns whether it wrote.
func writeIfChanged(out *output.Writer, path, content string) bool {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, []byte(content)) {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not create %s: %v", filepath.Dir(path), err))
		return false
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not write %s: %v", path, err))
		return false
	}
	return true
}

// firstProxyURL returns the first proxy URL set in env (any casing), or "".
func firstProxyURL(env map[string]string) string {
	for _, k := range proxyEnvKeys {
		if v := env[k]; v != "" {
			return v
		}
	}
	return ""
}

// proxyURLWithHost rewrites the host of a proxy URL, preserving scheme,
// credentials, and port. Returns "" for empty input.
func proxyURLWithHost(raw, host string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if p := u.Port(); p != "" {
		u.Host = host + ":" + p
	} else {
		u.Host = host
	}
	return u.String()
}

// proxyURLHostname returns the hostname of a proxy URL, or "".
func proxyURLHostname(raw string) string {
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		return u.Hostname()
	}
	return ""
}

// prependPythonPath ensures dir is the first entry on PYTHONPATH in env,
// comparing whole path elements (not substrings) so a prefix collision doesn't
// wrongly suppress it.
func prependPythonPath(env []string, dir string) []string {
	const key = "PYTHONPATH="
	sep := string(os.PathListSeparator)
	for i, kv := range env {
		if strings.HasPrefix(kv, key) {
			switch existing := kv[len(key):]; {
			case existing == "":
				env[i] = key + dir
			case !slices.Contains(strings.Split(existing, sep), dir):
				env[i] = key + dir + sep + existing
			}
			return env
		}
	}
	return append(env, key+dir)
}

// maybeInstallGatewayHook installs the gateway detection hook script and
// registers it in the agent's hook registration file so the agent knows the
// gateway is active without needing to run any visible checks. Claude
// Code-style agents read hook registrations from <baseDir>/settings.json;
// agents with a dedicated hooks file (e.g. Codex's ~/.codex/hooks.json) use
// the same schema in that file instead.
func maybeInstallGatewayHook(out *output.Writer, agentName, baseDir, hooksFile string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	// Write the hook script.
	hookPath := filepath.Join(home, baseDir, "hooks", "UserPromptSubmit", "onecli_gateway_detect.sh")
	existing, err := os.ReadFile(hookPath)
	if err != nil || !bytes.Equal(existing, []byte(gatewayDetectHook)) {
		if err := os.MkdirAll(filepath.Dir(hookPath), 0o750); err != nil {
			return
		}
		if err := os.WriteFile(hookPath, []byte(gatewayDetectHook), 0o755); err != nil {
			return
		}
	}

	registrationPath := filepath.Join(home, baseDir, "settings.json")
	includeMatcher := true
	if hooksFile != "" {
		// Dedicated hooks files omit the matcher field, matching the entries
		// the agent itself writes there.
		registrationPath = filepath.Join(home, filepath.FromSlash(hooksFile))
		includeMatcher = false
	}
	changed, err := registerUserPromptHook(registrationPath, "bash "+hookPath, includeMatcher)
	if err != nil || !changed {
		return
	}
	notice := fmt.Sprintf("onecli: installed gateway hook for %s.", agentName)
	if hooksFile != "" {
		// Dedicated-hooks-file agents gate new hooks behind a one-time trust
		// review; until approved, the hook is silently skipped.
		notice += fmt.Sprintf(" %s asks once before running new hooks; run /hooks inside it to trust the OneCLI gateway hook.", agentName)
	}
	out.Stderr(notice)
}

// registerUserPromptHook adds a UserPromptSubmit command hook to a Claude
// Code-style hook registration JSON file ({"hooks": {"UserPromptSubmit":
// [...]}}), preserving all other keys and hook events. Returns whether the
// file was changed; already-registered commands are left untouched.
func registerUserPromptHook(registrationPath, hookCommand string, includeMatcher bool) (bool, error) {
	settings := make(map[string]any)
	data, readErr := os.ReadFile(registrationPath)
	if readErr == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return false, err
		}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = make(map[string]any)
	}

	entries, _ := hooks["UserPromptSubmit"].([]any)

	// Check if our hook is already registered.
	for _, entry := range entries {
		e, _ := entry.(map[string]any)
		innerHooks, _ := e["hooks"].([]any)
		for _, h := range innerHooks {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); cmd == hookCommand {
				return false, nil
			}
		}
	}

	// Add our hook entry.
	entry := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": hookCommand,
			},
		},
	}
	if includeMatcher {
		entry["matcher"] = ""
	}
	entries = append(entries, entry)
	hooks["UserPromptSubmit"] = entries
	settings["hooks"] = hooks

	if err := os.MkdirAll(filepath.Dir(registrationPath), 0o750); err != nil {
		return false, err
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(registrationPath, append(encoded, '\n'), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// resolveGUIAppBinary returns the executable inside a GUI editor's macOS
// .app bundle. Under --enforce we must exec THIS, not the `cursor` CLI
// launcher: the launcher just asks macOS to open/focus the app, so
// sandboxing it would confine a process that exits immediately while the
// real editor runs ungoverned.
func resolveGUIAppBinary(spec agentSpec) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("sandboxed GUI launch is implemented for macOS only (got %s)", runtime.GOOS)
	}
	if spec.appBundle == "" {
		return "", fmt.Errorf("no .app bundle known for %s", spec.agentName)
	}
	// Search the standard locations; a user-local install is common.
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join("/Applications", spec.appBundle+".app", "Contents", "MacOS", spec.appBundle),
	}
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Applications", spec.appBundle+".app", "Contents", "MacOS", spec.appBundle))
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("could not find %s.app (looked in /Applications and ~/Applications)", spec.appBundle)
}

// guiAlreadyRunning reports whether the GUI editor is already running, and
// its pid. This matters for correctness, not convenience: macOS activates
// the existing instance rather than starting a second one, so launching
// "under the sandbox" while an unsandboxed copy is open would report
// enforcement while the user keeps using an ungoverned editor.
func guiAlreadyRunning(spec agentSpec) (bool, int) {
	if runtime.GOOS != "darwin" || spec.appBundle == "" {
		return false, 0
	}
	out, err := exec.Command("/usr/bin/pgrep", "-x", spec.appBundle).Output()
	if err != nil {
		return false, 0 // non-zero exit = not running
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return false, 0
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return false, 0
	}
	return true, pid
}

// clearElectronProxySettings removes the app-level proxy keys a previous
// (non-enforce) run injected. Under --enforce the editor is launched with
// a native --proxy-server flag instead, and leaving the settings in place
// breaks it two ways: a stale port from an earlier run wins over the flag
// and the sandbox denies it, and http.proxyAuthorization makes Electron's
// SimpleURLLoader reject requests outright with ERR_INVALID_ARGUMENT.
// Only OneCLI's own keys are touched; the rest of settings.json is
// preserved.
func clearElectronProxySettings(out *output.Writer, configDir string) {
	path := vscodeSettingsPath(configDir)
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return // no settings file: nothing injected, nothing to clear
	}
	settings := make(map[string]any)
	if err := json.Unmarshal(data, &settings); err != nil {
		out.Stderr("onecli: warning: could not parse editor settings to clear stale proxy keys; if the editor cannot reach the network, remove http.proxy and http.proxyAuthorization manually")
		return
	}
	changed := false
	for _, k := range []string{"http.proxy", "http.proxyAuthorization"} {
		if _, present := settings[k]; present {
			delete(settings, k)
			changed = true
		}
	}
	if !changed {
		return
	}
	encoded, err := json.MarshalIndent(settings, "", "    ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not clear stale proxy settings: %v", err))
		return
	}
	out.Stderr("onecli: cleared app-level proxy settings — under enforce the editor is pointed at the gateway by launch flag instead.")
}

// warnIfGatewayCANotTrusted checks that the gateway CA the *current* session
// will actually present is the one installed in the login keychain, and says
// exactly how to fix it when it isn't.
//
// This exists because the failure it catches is close to undebuggable from the
// symptom. Chromium/Electron reads the OS keychain and ignores SSL_CERT_FILE
// and NODE_EXTRA_CA_CERTS, so a GUI editor needs the CA installed there. If a
// previously-installed CA has since rotated, the keychain still contains a
// certificate named "OneCLI Local Gateway CA" — `security find-certificate`
// and `dump-trust-settings` both look correct — but its key no longer matches,
// so every request dies as net::ERR_CERT_AUTHORITY_INVALID and the editor's AI
// silently does nothing. Name equality is the trap, so this compares the
// public key, which is what actually has to match.
//
// Warn rather than fail: a stale CA breaks GUI editors, but the sandbox and
// the gateway are still fully enforcing, and CLI agents (which trust the
// bundle by env var) work fine. Refusing to launch would be a worse trade.
func warnIfGatewayCANotTrusted(out *output.Writer, gatewayPEM string) {
	if runtime.GOOS != "darwin" || strings.TrimSpace(gatewayPEM) == "" {
		return
	}
	live, err := publicKeyOfPEM([]byte(gatewayPEM))
	if err != nil {
		return
	}
	// Ask the keychain for every cert with this name: a rotation can leave
	// several, and the session works if ANY of them is the live one.
	cmd := exec.Command("/usr/bin/security", "find-certificate",
		"-c", gatewayCACommonName, "-a", "-p")
	installed, err := cmd.Output()
	if err != nil || len(installed) == 0 {
		out.Stderr("onecli: note: the gateway CA is not installed in your login keychain. " +
			"GUI editors (Cursor, VS Code) will fail with ERR_CERT_AUTHORITY_INVALID. Install it with:\n" +
			"  " + gatewayCATrustCommand)
		return
	}
	for _, block := range splitPEMCerts(installed) {
		if key, err := publicKeyOfPEM(block); err == nil && bytes.Equal(key, live) {
			// Right CA is present. Presence is NOT trust, though: a cert can
			// sit in the keychain with no trust settings at all, which is
			// exactly what `add-trusted-cert -d` does for a non-root user —
			// it writes to the ADMIN domain, silently applies nothing, and
			// leaves a certificate that every name-based check reports as
			// installed while Chromium still fails the handshake. Confirmed
			// on a real machine: `dump-trust-settings` listed only an
			// unrelated cert while Cursor logged ERR_CERT_AUTHORITY_INVALID
			// on every request. So ask the OS to actually evaluate it.
			if gatewayCAIsTrustedByOS() {
				return
			}
			out.Stderr("onecli: warning: the gateway CA is in your keychain but has no trust settings, " +
				"so GUI editors will still fail with ERR_CERT_AUTHORITY_INVALID.\n" +
				"  This is usually the result of using `-d` (admin domain) without root. Fix with:\n" +
				"    " + gatewayCATrustCommand)
			return
		}
	}
	out.Stderr("onecli: warning: a certificate named " + gatewayCACommonName + " is in your keychain, " +
		"but it is NOT the CA this gateway is using (the gateway CA has rotated since you trusted it).\n" +
		"  GUI editors will fail with ERR_CERT_AUTHORITY_INVALID until you re-trust it:\n" +
		"    security delete-certificate -c \"" + gatewayCACommonName + "\"\n" +
		"    " + gatewayCATrustCommand)
}

const gatewayCACommonName = "OneCLI Local Gateway CA"

// gatewayCATrustCommand is the exact command that works. Note the absence of
// `-d`: that selects the ADMIN trust domain, which needs root. Run as a normal
// user it adds the certificate and applies NO trust settings, producing a
// keychain entry that looks installed and still fails every TLS handshake.
const gatewayCATrustCommand = "security add-trusted-cert -r trustRoot " +
	"-k ~/Library/Keychains/login.keychain-db ~/.onecli/gateway-ca.pem"

// gatewayCAIsTrustedByOS asks macOS whether the gateway CA carries user-domain
// trust settings, rather than inferring trust from the certificate's presence.
// Presence and trust are genuinely different states here, and only the second
// one makes Chromium work.
func gatewayCAIsTrustedByOS() bool {
	out, err := exec.Command("/usr/bin/security", "dump-trust-settings").Output()
	if err != nil {
		// Exits non-zero when the domain holds no trust settings at all,
		// which is itself the untrusted answer.
		return false
	}
	return strings.Contains(string(out), gatewayCACommonName)
}

// publicKeyOfPEM returns a stable encoding of the certificate's public key.
// Comparing the key (not the fingerprint) is deliberate: a CA that is re-issued
// with the same key is still the same trust anchor and should not warn.
func publicKeyOfPEM(pemBytes []byte) ([]byte, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return x509.MarshalPKIXPublicKey(cert.PublicKey)
}

// splitPEMCerts splits a concatenated PEM stream into individual blocks.
func splitPEMCerts(data []byte) [][]byte {
	var out [][]byte
	rest := data
	for {
		block, remainder := pem.Decode(rest)
		if block == nil {
			return out
		}
		out = append(out, pem.EncodeToMemory(block))
		rest = remainder
	}
}

// injectElectronProxySettings writes http.proxy and http.proxyAuthorization
// into a VS Code-style settings.json so Electron-based editors authenticate
// with the gateway proxy without Chromium's native auth dialog. Returns the
// env with credentials stripped from proxy URLs.
func injectElectronProxySettings(out *output.Writer, env []string, configDir string, caPath string) []string {
	proxyURL := findProxyURL(env)
	if proxyURL == "" {
		return env
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.User == nil {
		return env
	}
	password, hasPass := u.User.Password()
	if !hasPass {
		return env
	}

	clean := *u
	clean.User = nil
	authValue := "Basic " + base64.StdEncoding.EncodeToString(
		[]byte(u.User.Username()+":"+password),
	)

	// Terminal env gets the full proxy URL (with credentials) since CLI
	// tools like curl and python handle embedded auth fine. Also inject
	// CA trust paths so TLS verification works through the proxy.
	terminalEnv := map[string]string{
		"HTTPS_PROXY": proxyURL,
		"HTTP_PROXY":  proxyURL,
	}
	if caPath != "" {
		for _, k := range caTrustKeys {
			terminalEnv[k] = caPath
		}
	}

	settingsPath := vscodeSettingsPath(configDir)
	if settingsPath == "" {
		return env
	}
	if err := mergeVSCodeProxySettings(settingsPath, clean.String(), authValue, terminalEnv); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not inject proxy settings: %v", err))
		return env
	}
	return stripProxyCredentials(env)
}

func findProxyURL(env []string) string {
	for _, key := range proxyEnvKeys {
		prefix := key + "="
		for _, kv := range env {
			if strings.HasPrefix(kv, prefix) {
				return kv[len(prefix):]
			}
		}
	}
	return ""
}

func vscodeSettingsPath(configDir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", configDir, "User", "settings.json")
	case "linux":
		return filepath.Join(home, ".config", configDir, "User", "settings.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), configDir, "User", "settings.json")
	default:
		return ""
	}
}

// Note: re-serialization via json.MarshalIndent sorts keys alphabetically.
func mergeVSCodeProxySettings(path, proxyURL, authHeader string, terminalEnv map[string]string) error {
	settings := make(map[string]any)
	data, readErr := os.ReadFile(path)
	if readErr == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("settings contains comments or invalid JSON; cannot merge proxy config")
		}
	}
	settings["http.proxy"] = proxyURL
	settings["http.proxyAuthorization"] = authHeader

	if len(terminalEnv) > 0 {
		termKey := "terminal.integrated.env.osx"
		switch runtime.GOOS {
		case "linux":
			termKey = "terminal.integrated.env.linux"
		case "windows":
			termKey = "terminal.integrated.env.windows"
		}
		existing, _ := settings[termKey].(map[string]any)
		if existing == nil {
			existing = make(map[string]any)
		}
		for k, v := range terminalEnv {
			existing[k] = v
		}
		settings[termKey] = existing
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating settings dir: %w", err)
	}
	out, err := json.MarshalIndent(settings, "", "    ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func stripProxyCredentials(env []string) []string {
	result := make([]string, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 || !slices.Contains(proxyEnvKeys, kv[:i]) {
			result = append(result, kv)
			continue
		}
		u, err := url.Parse(kv[i+1:])
		if err != nil || u.User == nil {
			result = append(result, kv)
			continue
		}
		u.User = nil
		result = append(result, kv[:i+1]+u.String())
	}
	return result
}
