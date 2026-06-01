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
	"strings"
	"syscall"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

//go:embed skill_gateway_fallback.md
var gatewaySkillFallback string

//go:embed hook_gateway_detect.sh
var gatewayDetectHook string

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
	rewriteProxyEnvHosts(cfg.Env, gatewayHost)

	// The gateway proxy injects the API key at the HTTP level (x-api-key header).
	// Keeping it in the env triggers a first-run confirmation prompt in Claude Code.
	delete(cfg.Env, "ANTHROPIC_API_KEY")

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
	if name, dir, cfgDir, noHook, _, nativeProxy, ok := agentSkillDir(c.Args[0]); ok {
		skillContent := gatewaySkillFallback
		if fetched, err := client.GetGatewaySkill(newContext()); err == nil && fetched != "" {
			skillContent = fetched
		}
		maybeInstallGatewaySkill(out, name, dir, skillContent)
		if !noHook {
			maybeInstallGatewayHook(out, name, dir)
		}

		// Electron-based agents (e.g. Cursor) ignore embedded user:pass in
		// HTTPS_PROXY and show a native auth dialog. Inject proxy credentials
		// into the app's VS Code-style settings.json instead.
		if cfgDir != "" {
			env = injectElectronProxySettings(out, env, cfgDir, caPath)
		}

		// Agents with a native proxy config (e.g. Codex) need proxy_url
		// written to their TOML config and CODEX_CA_CERTIFICATE set.
		if nativeProxy != "" {
			maybeInjectNativeProxyConfig(out, name, nativeProxy, env, caPath)
		}
		if agentFramework == "codex" {
			maybeCreateCodexAuthStub(out, client)
		}
	} else {
		// Unknown agent — install the skill to ~/.onecli/skills/ so the
		// framework can discover it via ONECLI_GATEWAY_SKILL_PATH.
		skillContent := gatewaySkillFallback
		if fetched, err := client.GetGatewaySkill(newContext()); err == nil && fetched != "" {
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

// rewriteProxyEnvHosts replaces Docker-internal hostnames in proxy URL values
// with the given local host, keeping the port and credentials intact.
// Only rewrites values that look like proxy URLs (contain "://").
func rewriteProxyEnvHosts(env map[string]string, localHost string) {
	proxyKeys := map[string]bool{
		"HTTPS_PROXY": true, "HTTP_PROXY": true,
		"https_proxy": true, "http_proxy": true,
	}
	for k, v := range env {
		if !proxyKeys[k] {
			continue
		}
		u, err := url.Parse(v)
		if err != nil {
			continue
		}
		if !dockerInternalHosts[u.Hostname()] {
			continue
		}
		port := u.Port()
		if port != "" {
			u.Host = localHost + ":" + port
		} else {
			u.Host = localHost
		}
		env[k] = u.String()
	}
}

// supportedAgents maps CLI binary base-names to (agentName, skillsBaseDir) pairs.
var supportedAgents = []struct {
	bases             []string
	agentName         string
	baseDir           string
	configDir         string // VS Code-style config dir name; non-empty enables proxy settings injection.
	skipHook          bool   // true for agents that don't support Claude Code-style hooks.
	hasPlugin         bool   // true for agents that support a transform_tool_result plugin.
	nativeProxyConfig string // home-relative dir containing a TOML config that needs proxy_url injection (e.g. ".codex").
}{
	{[]string{"claude"}, "Claude Code", ".claude", "", false, false, ""},
	{[]string{"cursor", "agent"}, "Cursor", ".cursor", "Cursor", false, false, ""},
	{[]string{"codex"}, "Codex", ".agents", "", false, false, ".codex"},
	{[]string{"hermes"}, "Hermes", ".hermes", "", true, true, ""},
	{[]string{"opencode"}, "OpenCode", ".opencode", "", false, false, ""},
}

// agentSkillDir returns the display name, skills base directory, and config
// options for a known agent command, or ok=false if the command is not recognized.
func agentSkillDir(cmd string) (agentName, baseDir, configDir string, skipHook bool, hasPlugin bool, nativeProxyConfig string, ok bool) {
	base := filepath.Base(cmd)
	for _, a := range supportedAgents {
		for _, b := range a.bases {
			if base == b {
				return a.agentName, a.baseDir, a.configDir, a.skipHook, a.hasPlugin, a.nativeProxyConfig, true
			}
		}
	}
	return "", "", "", false, false, "", false
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

// codexAuthStub is the auth.json stub written to ~/.codex/auth.json when the
// file does not exist. The id_token is a structurally valid JWT with email and
// plan_type claims so Codex's local validation passes. Real credentials are
// injected at the gateway proxy level.
const codexAuthStub = `{
  "auth_mode": "chatgpt",
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJvbmVjbGktbWFuYWdlZCIsImVtYWlsIjoib25lY2xpQG9uZWNsaS5zaCIsImV4cCI6NDEwMjQ0NDgwMCwiaWF0IjoxNzM1Njg5NjAwLCJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9wbGFuX3R5cGUiOiJmcmVlIiwiY2hhdGdwdF91c2VyX2lkIjoib25lY2xpLW1hbmFnZWQiLCJjaGF0Z3B0X2FjY291bnRfaWQiOiJvbmVjbGktbWFuYWdlZCJ9fQ.b25lY2xpLW1hbmFnZWQtc2lnbmF0dXJl",
    "access_token": "onecli-managed",
    "refresh_token": "onecli-managed",
    "account_id": "onecli-managed"
  },
  "last_refresh": "2025-01-01T00:00:00Z"
}
`

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

	content := codexAuthStub
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
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"} {
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
	proxyKeys := map[string]bool{
		"HTTPS_PROXY": true, "HTTP_PROXY": true,
		"https_proxy": true, "http_proxy": true,
	}
	result := make([]string, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 || !proxyKeys[kv[:i]] {
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
