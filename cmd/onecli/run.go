package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/onecli/onecli-cli/internal/api"
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/output"
	"github.com/onecli/onecli-cli/pkg/validate"
)

//go:embed skill_gateway.md
var gatewaySkill string

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
		return fmt.Errorf("command not found %s: %w", c.Args[0], err)
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

	// Write credential stubs for connected apps (non-fatal on failure).
	writeCredentialStubs(client, out)

	// Build child environment.
	env := buildChildEnv(os.Environ(), cfg.Env, caPath)

	// Install skill for known agents (silently updates stale files).
	// Fetch configured secrets to generate the dynamic services section.
	// Inject the agent name so the skill can reference it deterministically.
	if name, dir, ok := agentSkillDir(c.Args[0]); ok {
		project, err := resolveProject(c.Project)
		if err != nil {
			return err
		}
		secrets, _ := client.ListSecrets(newContext(), project)
		skillContent := buildSkillContent(secrets)
		maybeInstallGatewaySkill(out, name, dir, skillContent)
		env = append(env, "ONECLI_AGENT_NAME="+name)
		env = append(env, "ONECLI_URL="+config.APIHost())
	}

	// Exec — replaces this process so the agent gets direct terminal control.
	out.Stderr(fmt.Sprintf("onecli: gateway connected. Starting %s...", c.Args[0]))
	if err := syscall.Exec(binary, c.Args, env); err != nil {
		return fmt.Errorf("exec %s: %w", binary, err)
	}
	return nil
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
	for _, a := range supportedAgents {
		for _, b := range a.bases {
			if base == b {
				return a.agentName, a.baseDir, true
			}
		}
	}
	return "", "", false
}

// buildSkillContent generates the full skill file by replacing the
// {{SERVICES_SECTION}} placeholder in the embedded template with a
// dynamic section listing configured secrets.
func buildSkillContent(secrets []api.Secret) string {
	var sb strings.Builder
	sb.WriteString("## Your Gateway Services\n\n")

	// List API key secrets.
	var secretLines []string
	for _, s := range secrets {
		if s.HostPattern != "" {
			secretLines = append(secretLines, fmt.Sprintf("- %s (%s)", s.HostPattern, s.Name))
		}
	}
	if len(secretLines) > 0 {
		sb.WriteString("API key secrets configured for:\n")
		for _, line := range secretLines {
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("OAuth apps (Gmail, GitHub, Google Calendar, Google Drive, etc.) are\n")
	sb.WriteString("also available through the gateway. Just make the request directly;\n")
	sb.WriteString("the gateway injects credentials if the app is connected. If not, it\n")
	sb.WriteString("returns an error with a connect URL you can present to the user.\n")

	return strings.Replace(gatewaySkill, "{{SERVICES_SECTION}}", sb.String(), 1)
}

// credentialSentinel is the placeholder value used in credential stub files.
// Files containing this value are safe to overwrite; files without it contain
// real credentials and must not be touched.
const credentialSentinel = "onecli-managed"

// writeCredentialStubs fetches connected apps and writes credential stub files
// for apps that define them. Existing files with real credentials (no sentinel)
// are never overwritten.
func writeCredentialStubs(client *api.Client, out *output.Writer) {
	apps, err := client.ListApps(newContext())
	if err != nil {
		return
	}
	for _, app := range apps {
		if app.Connection == nil || app.Connection.Status != "connected" {
			continue
		}
		for _, stub := range app.CredentialStubs {
			if stub.Path == "" {
				continue
			}
			safeWriteStub(stub, out)
		}
	}
}

// safeWriteStub writes a single credential stub file. It refuses to overwrite
// files that don't contain the "onecli-managed" sentinel, and skips the write
// when the on-disk content already matches.
func safeWriteStub(stub api.CredentialStub, out *output.Writer) {
	path := expandTilde(stub.Path)

	content, err := json.MarshalIndent(stub.Content, "", "  ")
	if err != nil {
		return
	}
	content = append(content, '\n')

	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		if bytes.Equal(existing, content) {
			return
		}
		if !bytes.Contains(existing, []byte(credentialSentinel)) {
			out.Stderr(fmt.Sprintf("onecli: skipping %s (real credentials detected)", stub.Path))
			return
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not create directory for %s: %v", stub.Path, err))
		return
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		out.Stderr(fmt.Sprintf("onecli: warning: could not write credential stub %s: %v", stub.Path, err))
		return
	}
}

// expandTilde replaces a leading ~ with the user's home directory.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
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
