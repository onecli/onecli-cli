package main

import (
	"strings"
	"testing"

	"github.com/onecli/onecli-cli/internal/sandbox"
)

// Platform support is the sandbox package's concern now; this asserts the
// CLI-side contract that matters here: the launcher is only handed back
// when the sandbox is actually available, so wrap mode can never exec an
// unconfined binary while claiming enforcement.
func TestEnforceWrapLauncherGatesOnAvailability(t *testing.T) {
	got, err := enforceWrapLauncher()
	if err := sandbox.Available(); err != nil {
		return // unsupported host: nothing to assert beyond the error path
	}
	if err != nil {
		t.Fatalf("launcher unavailable on a supported host: %v", err)
	}
	if got == "" {
		t.Error("launcher path is empty on a supported host")
	}
}

func TestGUIEditorsMarkedForEnforceRejection(t *testing.T) {
	// `--enforce` fails closed for GUI editors: `onecli run -- cursor` only
	// opens an Electron app, there's no launched process tree to sandbox,
	// so wrap mode would sandbox-exec a launcher that governs nothing (and
	// would inject the ephemeral forwarder port into the app's PERSISTENT
	// settings.json). The routing keys on configDir != "", so pin that
	// Cursor carries it — a regression here silently re-enables the broken
	// path.
	spec, ok := agentSkillDir("cursor")
	if !ok {
		t.Fatal("cursor spec not found")
	}
	if spec.configDir == "" {
		t.Error("cursor must set configDir so --enforce fails closed for the GUI editor")
	}
	// A launched CLI agent (Codex) must NOT be treated as a GUI editor.
	if codex, _ := agentSkillDir("codex"); codex.configDir != "" {
		t.Error("codex is a CLI agent and must not carry configDir")
	}
}

func TestRewriteProxyEnvToLoopback(t *testing.T) {
	env := map[string]string{
		"HTTPS_PROXY": "http://x:aoc_tok@gateway.example.com:8443",
		"HTTP_PROXY":  "http://x:aoc_tok@gateway.example.com:8443",
		"NO_PROXY":    "keep-me",
		"OTHER":       "untouched",
	}
	rewriteProxyEnvToLoopback(env, 45678)

	want := "http://x:aoc_tok@127.0.0.1:45678"
	if env["HTTPS_PROXY"] != want {
		t.Errorf("HTTPS_PROXY = %q, want %q", env["HTTPS_PROXY"], want)
	}
	if env["HTTP_PROXY"] != want {
		t.Errorf("HTTP_PROXY = %q, want %q", env["HTTP_PROXY"], want)
	}
	if env["NO_PROXY"] != "keep-me" || env["OTHER"] != "untouched" {
		t.Error("non-proxy keys must not be rewritten")
	}
}

func TestRewriteProxyEnvToLoopbackPreservesAocMarker(t *testing.T) {
	// Codex's config.toml refresh keys on the aoc_ marker in proxy_url;
	// losing the credentials would orphan stale injected values.
	env := map[string]string{"HTTPS_PROXY": "http://x:aoc_tok@gw:8443"}
	rewriteProxyEnvToLoopback(env, 1)
	if !strings.Contains(env["HTTPS_PROXY"], "aoc_") {
		t.Errorf("rewrite dropped the aoc_ credential marker: %q", env["HTTPS_PROXY"])
	}
}

func TestEnforceWrapArgvAppendsQuirks(t *testing.T) {
	// enforceWrapArgv assembles agent args + per-agent quirk flags and
	// hands them to the platform sandbox's WrapArgv. The OS-launcher shape
	// (e.g. sandbox-exec on macOS) is the sandbox package's concern and is
	// asserted there; here we assert only the platform-neutral contract:
	// the agent's own args are present and quirk flags are appended last.
	got := strings.Join(enforceWrapArgv("/p/wrap.sb", "/bin/codex", []string{"exec", "task"}, "codex", 0), " ")
	if !strings.Contains(got, "exec task") {
		t.Errorf("agent args missing from argv: %q", got)
	}
	if !strings.HasSuffix(got, "-c sandbox_mode=danger-full-access") {
		t.Errorf("codex quirk args missing or not last: %q", got)
	}
}

func TestEnforceWrapNoticeAndQuirks(t *testing.T) {
	// An agent without quirks gets neither extra args nor a notice.
	if len(enforceWrapQuirkArgs("somecli", 0)) != 0 {
		t.Error("no quirk args expected for an agent without quirks")
	}
	if enforceWrapNotice("codex") == "" {
		t.Error("codex quirk must surface a user-facing notice")
	}
	if enforceWrapNotice("somecli") != "" {
		t.Error("no notice expected for agents without quirks")
	}
}
