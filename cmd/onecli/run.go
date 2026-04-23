package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

//go:embed skill_gateway.md
var gatewaySkill string

// RunCmd is `onecli run -- <command> [args...]`.
type RunCmd struct {
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
		return fmt.Errorf("command not found: %s", c.Args[0])
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

	// Install skill for known agents (silently updates stale files).
	if name, dir, ok := agentSkillDir(c.Args[0]); ok {
		maybeInstallGatewaySkill(out, name, dir)
	}

	// Dry-run: print resolved config and return.
	if c.DryRun {
		injected := make([]string, 0, len(cfg.Env)+6)
		for k := range cfg.Env {
			injected = append(injected, k)
		}
		if caPath != "" {
			injected = append(injected, "SSL_CERT_FILE", "REQUESTS_CA_BUNDLE",
				"CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "DENO_CERT")
		}
		return out.WriteDryRun("Would exec command with OneCLI gateway", map[string]any{
			"binary":       binary,
			"args":         c.Args,
			"env_injected": injected,
			"ca_path":      caPath,
		})
	}

	// Exec — replaces this process so the agent gets direct terminal control.
	out.Stderr(fmt.Sprintf("onecli: gateway connected. Starting %s...", c.Args[0]))
	return syscall.Exec(binary, c.Args, env)
}

// writeGatewayCACert writes the gateway CA PEM to ~/.onecli/gateway-ca.pem.
// Returns the path on success. Skips the write if on-disk content already matches.
func writeGatewayCACert(pem string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	caPath := filepath.Join(home, ".onecli", "gateway-ca.pem")
	if err := os.MkdirAll(filepath.Dir(caPath), 0o700); err != nil {
		return "", fmt.Errorf("creating CA dir: %w", err)
	}
	existing, err := os.ReadFile(caPath)
	if err == nil && bytes.Equal(existing, []byte(pem)) {
		return caPath, nil
	}
	if err := os.WriteFile(caPath, []byte(pem), 0o600); err != nil {
		return "", fmt.Errorf("writing CA cert: %w", err)
	}
	return caPath, nil
}

// proxyEnvKeys is the set of env keys that buildChildEnv may inject.
// Any pre-existing occurrence inherited from os.Environ() must be stripped
// before the new values are appended — POSIX getenv returns the first match,
// so a stale corporate HTTPS_PROXY from the parent shell would otherwise
// silently win and bypass the gateway.
var proxyEnvKeys = map[string]struct{}{
	"HTTPS_PROXY":               {},
	"HTTP_PROXY":                {},
	"https_proxy":               {},
	"http_proxy":                {},
	"NODE_EXTRA_CA_CERTS":       {},
	"NODE_USE_ENV_PROXY":        {},
	"GIT_TERMINAL_PROMPT":       {},
	"GIT_HTTP_PROXY_AUTHMETHOD": {},
	"SSL_CERT_FILE":             {},
	"REQUESTS_CA_BUNDLE":        {},
	"CURL_CA_BUNDLE":            {},
	"GIT_SSL_CAINFO":            {},
	"DENO_CERT":                 {},
	"ANTHROPIC_API_KEY":         {},
	"CLAUDE_CODE_OAUTH_TOKEN":   {},
}

// buildChildEnv builds the environment for the child process by stripping
// conflicting keys from the current env, appending the server-provided env,
// and overriding CA cert paths to use the local file.
func buildChildEnv(current []string, serverEnv map[string]string, caPath string) []string {
	// Build the combined set of keys to strip.
	stripKeys := make(map[string]struct{}, len(proxyEnvKeys)+len(serverEnv))
	for k := range proxyEnvKeys {
		stripKeys[k] = struct{}{}
	}
	for k := range serverEnv {
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

	// Append server-provided env (HTTPS_PROXY, credentials, etc.).
	for k, v := range serverEnv {
		out = append(out, k+"="+v)
	}

	// Append CA trust vars pointing to the local cert file, overriding the
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

// knownAgents maps CLI binary base-names to (agentName, skillsBaseDir) pairs.
var knownAgents = []struct {
	bases     []string
	agentName string
	baseDir   string
}{
	{[]string{"claude"}, "Claude Code", ".claude"},
	{[]string{"cursor", "agent"}, "Cursor", ".cursor"},
	{[]string{"codex"}, "Codex", ".agents"},
	{[]string{"hermes"}, "Hermes", ".hermes"},
	{[]string{"opencode"}, "OpenCode", ".opencode"},
}

// agentSkillDir returns the display name and skills base directory for a known
// agent command, or ok=false if the command is not recognized.
func agentSkillDir(cmd string) (agentName, baseDir string, ok bool) {
	base := filepath.Base(cmd)
	for _, a := range knownAgents {
		for _, b := range a.bases {
			if base == b {
				return a.agentName, a.baseDir, true
			}
		}
	}
	return "", "", false
}

// maybeInstallGatewaySkill installs the OneCLI gateway skill file if it is
// missing or stale. agentName is used in user-facing messages.
func maybeInstallGatewaySkill(out *output.Writer, agentName, baseDir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	fullPath := filepath.Join(home, baseDir, "skills", "onecli-gateway", "SKILL.md")

	existing, err := os.ReadFile(fullPath)
	if err == nil && bytes.Equal(existing, []byte(gatewaySkill)) {
		return
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not create skill directory: %v", err))
		return
	}
	if err := os.WriteFile(fullPath, []byte(gatewaySkill), 0o600); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not write skill file: %v", err))
		return
	}
	out.Stderr(fmt.Sprintf("onecli: installed gateway skill for %s.", agentName))
}
