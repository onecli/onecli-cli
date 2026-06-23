package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"

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
	Agent   string   `optional:"" name:"agent" help:"OneCLI agent identifier (uses default agent if omitted)."`
	Gateway string   `optional:"" name:"gateway" help:"Gateway host:port override (default: derived from API host)."`
	NoCA    bool     `optional:"" name:"no-ca" help:"Skip writing the CA cert and CA trust env injection."`
	DryRun  bool     `optional:"" name:"dry-run" help:"Print resolved env and command without executing."`
	Args    []string `arg:"" optional:"" name:"command" help:"Command and arguments to execute (after --)."`
}

func (c *RunCmd) Run(out *output.Writer) error {
	if len(c.Args) == 0 {
		return fmt.Errorf("no command specified: use 'onecli run -- <command> [args...]'")
	}

	// Validate agent identifier if provided.
	if c.Agent != "" {
		if err := validate.ResourceID(c.Agent); err != nil {
			return fmt.Errorf("invalid agent identifier: %w", err)
		}
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
	cfg, err := client.GetContainerConfig(newContext(), c.Agent)
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

	// Build child environment.
	env := buildChildEnv(os.Environ(), cfg.Env, caPath)

	env = append(env, "ONECLI_GATEWAY=true")

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
			maybeInstallGatewayHook(out, a.agentName, a.baseDir)
		}
		if a.pluginGateway {
			maybeInstallGatewayPlugin(out, a.agentName, a.baseDir)
		}

		// Electron-based agents (e.g. Cursor) ignore embedded user:pass in
		// HTTPS_PROXY and show a native auth dialog. Inject proxy credentials
		// into the app's VS Code-style settings.json instead.
		if a.configDir != "" {
			env = injectElectronProxySettings(out, env, a.configDir, caPath)
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

	// Exec — replaces this process so the agent gets direct terminal control.
	out.Stderr(fmt.Sprintf("onecli: gateway connected. Starting %s...", c.Args[0]))
	if err := syscall.Exec(binary, c.Args, env); err != nil {
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
	existing, err := os.ReadFile(caPath)
	if err == nil && bytes.Equal(existing, combined) {
		return caPath, nil
	}
	if err := os.WriteFile(caPath, combined, 0o600); err != nil {
		return "", fmt.Errorf("writing CA bundle: %w", err)
	}
	return caPath, nil
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
	skipHook          bool   // true for agents that don't support Claude Code-style UserPromptSubmit hooks.
	pluginGateway     bool   // true for agents that load the transform_tool_result recovery plugin (e.g. Hermes).
	dockerSandbox     bool   // true for agents that run tools in a Docker sandbox needing TERMINAL_DOCKER_* injection.
	nativeProxyConfig string // home-relative dir with a TOML config needing proxy_url injection (e.g. ".codex").
}

// supportedAgents maps CLI binary base-names to their gateway integration spec.
var supportedAgents = []struct {
	bases []string
	spec  agentSpec
}{
	{[]string{"claude"}, agentSpec{agentName: "Claude Code", baseDir: ".claude"}},
	{[]string{"cursor", "agent"}, agentSpec{agentName: "Cursor", baseDir: ".cursor", configDir: "Cursor"}},
	{[]string{"codex"}, agentSpec{agentName: "Codex", baseDir: ".agents", nativeProxyConfig: ".codex"}},
	{[]string{"hermes"}, agentSpec{agentName: "Hermes", baseDir: ".hermes", skipHook: true, pluginGateway: true, dockerSandbox: true}},
	{[]string{"opencode"}, agentSpec{agentName: "OpenCode", baseDir: ".opencode"}},
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
// agents that have their own managed proxy (e.g. Codex). Also sets the
// agent-specific CA certificate env var.
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
	content := string(data)

	// Inject [network] section with proxy_url if not already present.
	if !strings.Contains(content, "proxy_url") {
		section := "\n[network]\nproxy_url = \"" + proxyURL + "\"\n"
		content += section
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			out.Stderr(fmt.Sprintf("onecli: warning: could not write proxy config for %s: %v", agentName, err))
			return
		}
		out.Stderr(fmt.Sprintf("onecli: configured native proxy for %s.", agentName))
	}

	// Set CODEX_CA_CERTIFICATE if we have a CA path — Codex reads this
	// in addition to SSL_CERT_FILE for its Rust TLS client.
	if caPath != "" {
		os.Setenv("CODEX_CA_CERTIFICATE", caPath)
	}
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
// registers it in the agent's settings.json so the agent knows the gateway
// is active without needing to run any visible checks.
func maybeInstallGatewayHook(out *output.Writer, agentName, baseDir string) {
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

	// Register in settings.json if not already present.
	settingsPath := filepath.Join(home, baseDir, "settings.json")
	settings := make(map[string]any)
	data, readErr := os.ReadFile(settingsPath)
	if readErr == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return
		}
	}

	hookCommand := "bash " + hookPath
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
				return
			}
		}
	}

	// Add our hook entry.
	entries = append(entries, map[string]any{
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": hookCommand,
			},
		},
	})
	hooks["UserPromptSubmit"] = entries
	settings["hooks"] = hooks

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o750); err != nil {
		return
	}
	out2, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(settingsPath, append(out2, '\n'), 0o600); err != nil {
		return
	}
	out.Stderr(fmt.Sprintf("onecli: installed gateway hook for %s.", agentName))
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
