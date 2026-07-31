package main

// Enforce-wrap mode: the agent-agnostic half of `onecli run --enforce`.
//
// The native path (run_enforce.go) borrows Claude Code's own sandbox and
// therefore only ever covers Claude Code — and only its Bash tool. The
// wrap path inverts ownership: onecli brings its own OS sandbox and puts
// the ENTIRE agent process tree inside it, so any agent (Codex, Cursor,
// Gemini, a bare shell) gets the same guarantee, including its own model
// API calls and every subprocess it spawns.
//
// The OS mechanism lives in internal/sandbox, selected by build tag
// (Seatbelt today; Linux planned). This file holds only what is NOT
// OS-specific: repointing the proxy env at the loopback forwarder, and
// the per-agent quirks table. The profile itself deliberately has ONE
// definition, in that package: two copies of a security policy is how the
// docker.sock rule stayed broken while a text assertion kept passing.

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/onecli/onecli-cli/internal/sandbox"
)

// rewriteProxyEnvToLoopback repoints every proxy URL in env at the
// loopback forwarder, preserving scheme and credentials. Under the wrap
// the sandbox blocks a direct dial to the gateway host, so the proxy env
// (and everything derived from it: Codex's proxy_url TOML, Electron
// settings) must route through the forwarder. Credentials are kept
// intact — the forwarder overrides them anyway, and Codex's config
// refresh logic keys on the embedded aoc_ marker.
func rewriteProxyEnvToLoopback(env map[string]string, port uint16) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	for _, k := range proxyEnvKeys {
		v := env[k]
		if v == "" {
			continue
		}
		u, err := url.Parse(v)
		if err != nil {
			continue
		}
		u.Host = addr
		env[k] = u.String()
	}
}

// enforceWrapQuirkArgs returns extra argv appended for agents whose own
// runtime needs adjusting to live inside the wrap. Appended last so it
// wins over defaults (and is visible in ps for auditability).
// forwarderPort is the loopback port all sandboxed egress must exit
// through; Chromium-based agents need it as a native flag (see below).
func enforceWrapQuirkArgs(framework string, forwarderPort uint16) []string {
	switch framework {
	case "codex":
		// Codex's internal Seatbelt (sandbox_apply) gets EPERM inside an
		// outer Seatbelt profile, which breaks every shell command it
		// runs. Disable its inner sandbox: network governance moves to
		// the OneCLI sandbox (strictly stronger — whole process tree),
		// filesystem confinement falls back to Codex's approval flow
		// plus the deferred-egress denies in the profile.
		return []string{"-c", "sandbox_mode=danger-full-access"}
	case "cursor", "agent":
		args := []string{
			// Electron/Chromium spawns renderer, GPU and network child
			// processes that each try to create their OWN sandbox. Inside
			// our outer Seatbelt profile that call is denied ("sandbox
			// initialization failed: Operation not permitted"), the GPU
			// process dies repeatedly and Chromium aborts the app ("GPU
			// process isn't usable. Goodbye."). Disabling Chromium's inner
			// sandbox lets the editor run; egress stays confined by the
			// outer profile, which is the guarantee we actually make.
			// Same shape as the Codex quirk: one sandbox, ours, not two.
			"--no-sandbox",
		}
		if forwarderPort != 0 {
			// The editor's own network stack (Electron's SimpleURLLoader)
			// does NOT reliably honor VS Code's `http.proxy` setting — it
			// failed with net::ERR_INVALID_ARGUMENT while the identical
			// request succeeded through the same forwarder via curl. Since
			// we launch the binary ourselves, configure Chromium natively
			// instead of hoping the app-level setting is respected.
			args = append(args, fmt.Sprintf("--proxy-server=http://127.0.0.1:%d", forwarderPort))
		}
		return args
	}
	return nil
}

// enforceWrapNotice returns the agent-specific stderr notice for quirks
// that change the agent's own behavior, or "".
func enforceWrapNotice(framework string) string {
	switch framework {
	case "codex":
		return "onecli: Codex's internal sandbox is disabled under enforce — the OneCLI sandbox governs all egress instead; filesystem safety falls back to Codex approvals."
	case "cursor", "agent":
		return "onecli: Chromium's internal sandbox is disabled under enforce (it cannot nest inside ours) — the OneCLI sandbox governs all editor egress instead."
	}
	return ""
}

// enforceWrapArgv builds the argv that runs the agent confined: the
// launcher's argv with per-agent quirk flags appended last.
func enforceWrapArgv(profilePath, binary string, agentArgs []string, framework string, forwarderPort uint16) []string {
	args := append(append([]string{}, agentArgs...), enforceWrapQuirkArgs(framework, forwarderPort)...)
	return sandbox.WrapArgv(profilePath, binary, args)
}

// enforceWrapLauncher resolves the binary to exec for wrap mode.
func enforceWrapLauncher() (string, error) {
	if err := sandbox.Available(); err != nil {
		return "", err
	}
	return sandbox.LauncherPath(), nil
}

// resolveEnforceWrap prepares wrap mode: verifies platform support,
// spawns the loopback forwarder, repoints the proxy env at it, and
// materializes the sandbox profile. Returns the profile handle and the
// forwarder port. Must run BEFORE the child env is built — everything
// derived from cfg.Env has to carry the loopback URL. Fails closed, like
// the native path.
func resolveEnforceWrap(env map[string]string) (profilePath string, port uint16, err error) {
	if err := sandbox.Available(); err != nil {
		return "", 0, err
	}
	port, err = spawnEnforceForwarder(firstProxyURL(env))
	if err != nil {
		return "", 0, err
	}
	rewriteProxyEnvToLoopback(env, port)
	// The profile is rendered AFTER the forwarder exists: its network allow
	// is scoped to that exact port, which is what keeps the host's LAN
	// interfaces out of reach.
	profilePath, err = sandbox.Materialize(port)
	if err != nil {
		return "", 0, err
	}
	return profilePath, port, nil
}
