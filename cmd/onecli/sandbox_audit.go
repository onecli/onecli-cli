package main

// `onecli sandbox audit` — a one-command red-team of the enforce-mode
// sandbox.
//
// Enforce mode's whole promise is "egress cannot bypass the gateway".
// That promise is made of OS rules, and OS rules fail in ways that are
// invisible by inspection: a deny rule can be syntactically valid, load
// without error, and still never match. We shipped exactly that bug —
// `(path-literal "/var/run/docker.sock")` looked correct but never fired,
// because Seatbelt matches the RESOLVED path and Docker Desktop makes
// that path a symlink into ~/.docker/run. The profile "looked right" for
// as long as nobody tried the bypass.
//
// So the guarantee gets an executable test, runnable by anyone, on the
// machine that matters: this command runs each known escape technique
// inside the real profile and reports whether the OS actually stopped it.
//
// The probe table is the single source of truth: the live test
// (run_enforce_wrap_bypass_live_test.go) iterates the same table, so a
// vector added here is automatically covered in CI and by users.

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/onecli/onecli-cli/internal/sandbox"
	"github.com/onecli/onecli-cli/pkg/output"
)

// probeOutcome is what a probe is expected to do under a correct profile.
type probeOutcome int

const (
	// mustBlock: the OS must refuse this. A success is a sandbox hole.
	mustBlock probeOutcome = iota
	// mustAllow: legitimate local work that must keep functioning. A
	// failure means the profile is too tight and will break real usage —
	// which matters because an unusable sandbox gets turned off.
	mustAllow
)

// sandboxProbe is one escape technique (or one required capability).
type sandboxProbe struct {
	name string
	// why explains what the vector buys an attacker, so a failure report
	// tells the reader the stakes rather than just a rule name.
	why  string
	want probeOutcome
	argv []string
	// needs, when set, is a path that must exist for the probe to be
	// meaningful (e.g. the Docker socket). Absent → skipped, not failed.
	needs string
	// precondition, when set, reports whether the environment can exercise
	// this vector at all. It exists for the same reason as needs, but for
	// conditions that aren't a file: an IPv6 probe on an IPv4-only network
	// "fails" for lack of a route and would otherwise report a confident
	// ok while testing nothing. Skipping is honest; a false pass is not.
	precondition func() (ok bool, reason string)
}

// canSendAppleEvents reports whether Apple Events are usable at all here.
// macOS gates them behind TCC automation consent independently of any
// sandbox, so on a machine that has never granted it, the probe fails for
// a reason that has nothing to do with our deny rule — a false pass.
// TestProbeNotVacuous is what exposed this.
func canSendAppleEvents() (bool, string) {
	out, err := exec.Command("/usr/bin/osascript", "-e",
		`tell application "System Events" to get name of every process`).CombinedOutput()
	if err != nil && strings.Contains(string(out), "Not authorized") {
		return false, "Apple Events are TCC-denied on this host (the OS already blocks this vector)"
	}
	return true, ""
}

// hasIPv6Route reports whether this host can reach the IPv6 internet.
func hasIPv6Route() (bool, string) {
	// A UDP "connect" performs no handshake — it just resolves a route —
	// so this is instant and sends nothing.
	c, err := net.DialTimeout("udp6", "[2606:4700:4700::1111]:53", 2*time.Second)
	if err != nil {
		return false, "no IPv6 route on this network"
	}
	_ = c.Close()
	return true, ""
}

// lanRelayTarget starts a listener bound to ALL interfaces and returns the
// host's own LAN address for it, standing in for the unsandboxed relay a
// real bypass would use (a Docker published port, a dev server on
// 0.0.0.0). The audit owns the listener so the probe never depends on
// something happening to be running: a probe whose target is absent
// "passes" for the wrong reason, which is the failure TestProbesCanDetectHoles
// exists to reject. Returns "" when the host has no LAN interface.
func lanRelayTarget() (url string, stop func()) {
	out, err := exec.Command("/bin/bash", "-c",
		"ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1").Output()
	ip := strings.TrimSpace(string(out))
	if err != nil || ip == "" {
		return "", func() {}
	}
	// 0.0.0.0 so the socket is reachable via the LAN address, exactly like
	// the container that demonstrated the original exfil.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return "", func() {}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://%s:%d/exfil", ip, port), func() { _ = ln.Close() }
}

// sandboxProbes is the escape matrix. Every entry is a way to reach the
// network WITHOUT the loopback forwarder — none of them read HTTPS_PROXY,
// which is precisely why env-var "enforcement" is not enforcement.
//
// 1.1.1.1 is used as a raw IP so DNS is never involved: connect() itself
// must be refused, which also makes a denial instant and distinguishable
// from a network timeout.
// sandboxProbes uses port 0, meaning "no stand-in forwarder": callers that
// only need the escape matrix (tests) get every vector except the two that
// require a live allowed port.
// The relay listener is deliberately NOT closed here: the probes are run
// by the caller, after this returns, so closing it would leave the LAN
// probe pointing at a dead port — it would then "pass" because nothing
// was listening rather than because the sandbox blocked it. The listener
// is bound to an ephemeral port and dies with the process.
func sandboxProbes() []sandboxProbe {
	url, _ := lanRelayTarget()
	return sandboxProbesFor(0, url)
}

// sandboxProbesFor builds the matrix against a known forwarder port.
func sandboxProbesFor(forwarderPort uint16, lanRelayURL string) []sandboxProbe {
	direct := []sandboxProbe{
		{
			name: "direct-dial",
			why:  "plain curl to a raw IP — the baseline bypass",
			want: mustBlock,
			argv: []string{"/usr/bin/curl", "-sS", "--max-time", "5", "http://1.1.1.1/"},
		},
		{
			name: "proxy-env-stripped",
			why:  "agent unsets HTTPS_PROXY — enforcement must not depend on env it can edit",
			want: mustBlock,
			argv: []string{"/bin/bash", "-c",
				"unset HTTPS_PROXY HTTP_PROXY ALL_PROXY https_proxy http_proxy all_proxy; " +
					"/usr/bin/curl -sS --max-time 5 http://1.1.1.1/"},
		},
		{
			name: "raw-socket",
			why:  "a language runtime socket ignores proxy config entirely",
			want: mustBlock,
			argv: []string{"/usr/bin/python3", "-c",
				"import socket;socket.create_connection(('1.1.1.1',80),5)"},
		},
		{
			name: "child-process",
			why:  "a grandchild must inherit the boundary, or any wrapper script escapes",
			want: mustBlock,
			argv: []string{"/bin/bash", "-c", "/usr/bin/curl -sS --max-time 5 http://1.1.1.1/"},
		},
		{
			// THE vector a "localhost is safe" assumption misses. Seatbelt's
			// (remote ip "localhost:*") means any address LOCAL TO THIS
			// MACHINE, including the host's LAN interfaces — so any listener
			// bound to 0.0.0.0 (a Docker published port, a dev server on
			// your own laptop) is reachable, and being unsandboxed it
			// relays straight to the internet. Proven end-to-end against a
			// container before the allow was scoped to the forwarder port:
			// a secret left the machine while direct egress was blocked.
			name: "lan-interface-relay",
			why:  "a listener on the host's LAN IP is unsandboxed and relays to the internet",
			want: mustBlock,
			precondition: func() (bool, string) {
				if lanRelayURL == "" {
					return false, "no LAN interface to exercise the relay vector"
				}
				return true, ""
			},
			argv: []string{"/usr/bin/curl", "-sS", "--max-time", "4", "-o", "/dev/null", lanRelayURL},
		},
		{
			// Only meaningful where IPv6 actually routes. On an IPv4-only
			// network this fails for lack of a route and would report a
			// confident "ok" while proving nothing, so it is gated on a
			// working v6 path (see needsIPv6) and skipped otherwise.
			// TestProbeNotVacuous is what surfaced this.
			name:         "ipv6-literal",
			why:          "an IPv4-only rule is sidestepped by dialing the v6 address",
			want:         mustBlock,
			precondition: hasIPv6Route,
			argv:         []string{"/usr/bin/curl", "-sS", "--max-time", "5", "-g", "http://[2606:4700:4700::1111]/"},
		},
		{
			name: "udp-dns-exfil",
			why:  "DNS to an off-box resolver is a covert channel, not name lookup",
			want: mustBlock,
			argv: []string{"/usr/bin/python3", "-c",
				"import socket;s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);" +
					"s.settimeout(5);s.sendto(b'x',('8.8.8.8',53))"},
		},
		{
			name: "launchservices-open",
			why:  "`open URL` launches a browser OUTSIDE the sandbox — fetch-and-exfil",
			want: mustBlock,
			argv: []string{"/usr/bin/open", "https://example.com"},
		},
		{
			// Uses System Events rather than Safari: driving Safari trips
			// the macOS TCC automation prompt and fails for that reason
			// even with no sandbox at all, which made the old probe
			// vacuous. This form exercises the appleevent-send deny itself.
			name:         "appleevent-fetch",
			why:          "Apple Events drive apps OUTSIDE the sandbox, which can fetch URLs for you",
			want:         mustBlock,
			precondition: canSendAppleEvents,
			argv: []string{"/usr/bin/osascript", "-e",
				`tell application "System Events" to get name of every process`},
		},
		{
			name:  "docker-socket",
			why:   "a container runs OUTSIDE the sandbox: one line to unrestricted egress",
			want:  mustBlock,
			needs: "/var/run/docker.sock",
			argv: []string{"/usr/bin/curl", "-sS", "--max-time", "5",
				"--unix-socket", "/var/run/docker.sock", "http://localhost/version"},
		},
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// Without a home dir the deferred-egress paths can't be
		// materialized. Return the direct matrix rather than silently
		// grading fewer vectors than the caller believes were tried — a
		// short audit that still prints PASS is worse than an error.
		return direct
	}
	probes := append(direct, deferredEgressProbes(home)...)
	probes = append(probes, requiredCapabilities(home)...)
	if forwarderPort != 0 {
		probes = append(probes, sandboxProbe{
			// The counterpart to lan-interface-relay: scoping the allow to
			// one port must not sever the one path that has to work.
			name: "forwarder-reachable",
			why:  "the loopback forwarder is the ONLY sanctioned egress path; severing it breaks everything",
			want: mustAllow,
			argv: []string{"/usr/bin/curl", "-sS", "--max-time", "4", "-o", "/dev/null",
				fmt.Sprintf("http://127.0.0.1:%d/", forwarderPort)},
		})
	}
	return probes
}

// deferredEgressProbes are the write-now, run-later vectors: the
// sandboxed process cannot dial out, so instead it plants something an
// UNSANDBOXED process executes later — and that process has unrestricted
// egress. They are network bypasses with a delay, which is why a
// network-only profile never actually delivered a network guarantee.
// Every one of these succeeded before the filesystem rules existed.
func deferredEgressProbes(home string) []sandboxProbe {
	// plant tests whether path is WRITABLE, via a zero-byte append.
	//
	// The obvious form (`echo probe > path`) is unacceptable here: these
	// paths include the user's real ~/.zshrc, and a truncating redirect
	// would destroy it on exactly the machines where the sandbox has the
	// hole being tested for. A security audit must not be able to damage
	// the system it audits.
	//
	// `>>` opens for append and writes nothing: the open() is the entire
	// test — EPERM under a correct profile, success under a holed one —
	// while existing content is untouched and any file the probe creates
	// is empty and inert.
	plant := func(name, why, path string) sandboxProbe {
		return sandboxProbe{
			name: name, why: why, want: mustBlock,
			argv: []string{"/bin/bash", "-c", fmt.Sprintf("printf '' >> %q", path)},
		}
	}
	return []sandboxProbe{
		plant("plant-launchagent",
			"launchd runs a LaunchAgent at every login, entirely outside the sandbox",
			home+"/Library/LaunchAgents/onecli-audit-probe.plist"),
		plant("write-shell-rc",
			"the user's next terminal sources this and runs it unsandboxed",
			home+"/.zshrc"),
		plant("plant-path-binary",
			"a binary on $PATH gets executed later by an unsandboxed shell",
			"/usr/local/bin/onecli-audit-probe"),
		// Targets the REAL artifact, not a made-up filename under the
		// directory. The earlier version probed a path the product never
		// writes, so it passed against a broad subpath deny that was in
		// fact breaking `onecli config set` — a probe can only defend the
		// exact path it names.
		plant("rewrite-sandbox-profile",
			"rewriting the profile disables enforcement on the very next run",
			home+"/.onecli/enforce-wrap.sb"),
		plant("rewrite-enforce-settings",
			"rewriting the Claude enforce settings disables the native enforce path",
			home+"/.onecli/enforce-sandbox-settings.json"),
		{
			name:  "read-ssh-keys",
			why:   "private keys are the payload any exfil attempt is after",
			want:  mustBlock,
			needs: home + "/.ssh",
			argv:  []string{"/bin/bash", "-c", fmt.Sprintf("ls %q > /dev/null", home+"/.ssh")},
		},
		// Git probes build a THROWAWAY repo in the temp dir first, then
		// write into its .git. An earlier version targeted `.git/hooks` in
		// the process's cwd, which didn't exist under the test harness — so
		// the write failed for the wrong reason and the probe reported
		// "blocked" while the rule was entirely absent. A probe that can
		// pass without exercising the rule is worse than no probe, so each
		// one creates the target it needs.
		{
			name: "plant-git-hook",
			why:  "a pre-commit hook runs UNSANDBOXED on the user's very next commit",
			want: mustBlock,
			argv: []string{"/bin/bash", "-c",
				"d=$(mktemp -d) && /usr/bin/git init -q \"$d\" && " +
					"printf '' >> \"$d/.git/hooks/pre-commit\""},
		},
		{
			// The .sample carve-out is the load-bearing exception in the
			// hooks rule (without it `git clone` fails). Promoting a sample
			// to a live hook name is the obvious way to abuse it, so the
			// carve-out gets its own probe rather than being trusted.
			name: "promote-sample-hook",
			why:  "copying pre-commit.sample to pre-commit would turn the clone carve-out into a hook",
			want: mustBlock,
			argv: []string{"/bin/bash", "-c",
				"d=$(mktemp -d) && /usr/bin/git init -q \"$d\" && " +
					"cp \"$d/.git/hooks/pre-commit.sample\" \"$d/.git/hooks/pre-commit\""},
		},
		{
			name: "global-git-hookspath",
			why:  "core.hooksPath in the GLOBAL gitconfig redirects hooks for every repo at once",
			want: mustBlock,
			argv: []string{"/bin/bash", "-c",
				"/usr/bin/git config --global core.hooksPath /tmp/onecli-audit-probe"},
		},
	}
}

// requiredCapabilities are legitimate operations the profile must NOT
// break. They belong in a security audit because an unusable sandbox gets
// switched off, and a sandbox that is off enforces nothing: "too tight"
// is a real failure mode, not a nitpick.
func requiredCapabilities(home string) []sandboxProbe {
	mkdirProbe := func(name, why, dir string) sandboxProbe {
		return sandboxProbe{
			name: name, why: why, want: mustAllow,
			argv: []string{"/bin/bash", "-c",
				fmt.Sprintf("mkdir -p %q && rmdir %q", dir, dir)},
		}
	}
	return []sandboxProbe{
		{
			name: "dns-resolution",
			why:  "local DNS over unix IPC must keep working or every tool breaks",
			want: mustAllow,
			argv: []string{"/usr/bin/dscacheutil", "-q", "host", "-a", "name", "localhost"},
		},
		mkdirProbe("home-cache-writes",
			"package managers and language toolchains write under $HOME constantly",
			home+"/.cache/onecli-audit-probe"),
		mkdirProbe("agent-state-writes",
			"agents write session state every run; breaking it breaks the agent",
			home+"/.codex/onecli-audit-probe"),
		{
			name: "onecli-config-readable",
			why:  "the agent must still read its own config",
			want: mustAllow,
			argv: []string{"/bin/bash", "-c", fmt.Sprintf("ls %q > /dev/null", home+"/.onecli")},
		},
		{
			// onecli's own state lives beside the enforcement artifacts, so
			// self-protection rules can silently break the product. A broad
			// ~/.onecli deny did exactly that; only this probe distinguishes
			// "protecting the profile" from "bricking config set".
			name: "onecli-config-writable",
			why:  "onecli config set / auth login must work from inside an enforced session",
			want: mustAllow,
			argv: []string{"/bin/bash", "-c",
				fmt.Sprintf("printf '' >> %q", home+"/.onecli/onecli-audit-probe.tmp")},
		},
		{
			name: "git-operations",
			why:  "git is the agent's primary tool; breaking it makes enforce unusable",
			want: mustAllow,
			argv: []string{"/usr/bin/git", "--version"},
		},
		{
			// These two exist because the first draft of the git-hook deny
			// broke BOTH of them: git copies template hooks into every new
			// .git/hooks, and writes .git/config to create a repository. A
			// rule that stops an agent from cloning is a worse bug than the
			// hook it prevents, and only a mustAllow probe catches that.
			name: "git-clone",
			why:  "cloning is table stakes; a hooks rule that blocks it is worse than the vector",
			want: mustAllow,
			argv: []string{"/bin/bash", "-c",
				"s=$(mktemp -d) && /usr/bin/git init -q \"$s\" && " +
					"d=$(mktemp -d) && /usr/bin/git clone -q \"$s\" \"$d/clone\" && " +
					"test -d \"$d/clone/.git\""},
		},
		{
			name: "git-init-and-commit",
			why:  "creating a repo and committing must work, or the agent cannot do its job",
			want: mustAllow,
			argv: []string{"/bin/bash", "-c",
				"d=$(mktemp -d) && cd \"$d\" && /usr/bin/git init -q . && echo x > f && " +
					"/usr/bin/git add f && " +
					"/usr/bin/git -c user.email=a@b -c user.name=c commit -qm probe"},
		},
	}
}

// probeResult is one probe's verdict.
type probeResult struct {
	probe   sandboxProbe
	skipped bool
	// held reports whether the guarantee held: the probe did what the
	// profile requires (blocked what must block, allowed what must allow).
	held    bool
	detail  string
	elapsed time.Duration
}

// runSandboxProbe executes one probe under profilePath.
//
// A blocked probe must also be FAST. Seatbelt refuses connect()
// immediately, whereas an unreachable network hangs until the timeout —
// both surface as a non-zero exit, so without the timing check a machine
// that is simply offline would look perfectly sandboxed. That false
// "secure" reading is worse than no test at all.
func runSandboxProbe(profilePath string, p sandboxProbe) probeResult {
	if p.needs != "" {
		if _, err := os.Stat(p.needs); err != nil {
			return probeResult{probe: p, skipped: true, held: true,
				detail: fmt.Sprintf("%s not present on this host", p.needs)}
		}
	}
	if p.precondition != nil {
		if ok, reason := p.precondition(); !ok {
			return probeResult{probe: p, skipped: true, held: true, detail: reason}
		}
	}

	argv := append([]string{"-f", profilePath}, p.argv...)
	start := time.Now()
	out, err := exec.Command(sandbox.LauncherPath(), argv...).CombinedOutput()
	elapsed := time.Since(start)
	res := probeResult{probe: p, elapsed: elapsed}

	switch p.want {
	case mustBlock:
		if err == nil {
			res.held = false
			res.detail = "SUCCEEDED — the sandbox did not stop it: " + firstLine(out)
			return res
		}
		if elapsed > 6*time.Second {
			res.held = false
			res.detail = fmt.Sprintf("failed only after %v — looks like a network timeout, "+
				"not a sandbox denial; re-run with network up", elapsed.Round(time.Millisecond))
			return res
		}
		res.held = true
		res.detail = "refused by the OS in " + elapsed.Round(time.Millisecond).String()
	case mustAllow:
		if err != nil {
			res.held = false
			res.detail = "BROKEN by the profile: " + firstLine(out)
			return res
		}
		res.held = true
		res.detail = "works"
	}
	return res
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 100 {
		s = s[:100] + "…"
	}
	if s == "" {
		return "(no output)"
	}
	return s
}

// SandboxCmd groups sandbox inspection commands.
type SandboxCmd struct {
	Audit SandboxAuditCmd `cmd:"" help:"Red-team the enforce-mode sandbox: try every known bypass and report which the OS actually stops."`
}

// SandboxAuditCmd runs the escape matrix against a real profile.
type SandboxAuditCmd struct {
	Agent string `arg:"" optional:"" help:"Agent to audit (codex, cursor, claude, ...). Defaults to the OneCLI-owned sandbox used by every non-native agent."`
}

// Run executes the audit.
func (c *SandboxAuditCmd) Run() error {
	out := output.New()

	if err := sandbox.Available(); err != nil {
		return err
	}

	// Claude Code takes the NATIVE path: enforcement is delegated to its
	// own sandbox, configured via --settings, which this process cannot
	// invoke standalone. Saying so plainly beats auditing the wrong
	// profile and reporting a guarantee that isn't the one in effect.
	if c.Agent != "" {
		if spec, known := agentSkillDir(c.Agent); known && enforceSupportedAgents[spec.agentName] {
			return c.reportNative(out, spec.agentName)
		}
	}

	// The profile's network allow is scoped to the forwarder's port, so an
	// audit needs a stand-in listener to represent it. Binding one here
	// (rather than hardcoding a port) means the "loopback reachable" probe
	// tests a real socket, and every other destination is genuinely outside
	// the allow — including the host's own LAN interfaces.
	stand, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("binding a stand-in forwarder: %w", err)
	}
	defer func() { _ = stand.Close() }()
	go func() {
		for {
			c, err := stand.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
			_ = c.Close()
		}
	}()
	forwarderPort := uint16(stand.Addr().(*net.TCPAddr).Port)

	// A relay the audit owns, standing in for the unsandboxed listener a
	// real bypass would abuse. Owning it means the probe can never pass
	// merely because nothing happened to be listening.
	lanURL, stopLAN := lanRelayTarget()
	defer stopLAN()

	profile, err := sandbox.Materialize(forwarderPort)
	if err != nil {
		return fmt.Errorf("writing sandbox profile: %w", err)
	}

	label := "the OneCLI sandbox (wrap mode)"
	if c.Agent != "" {
		if spec, known := agentSkillDir(c.Agent); known {
			label = spec.agentName + " under the OneCLI sandbox (wrap mode)"
		}
	}
	out.Stderr(fmt.Sprintf("Auditing %s", label))
	out.Stderr(fmt.Sprintf("Profile: %s", profile))
	out.Stderr("")

	var holes, broken int
	for _, p := range sandboxProbesFor(forwarderPort, lanURL) {
		res := runSandboxProbe(profile, p)
		switch {
		case res.skipped:
			out.Stderr(fmt.Sprintf("  SKIP  %-22s %s", p.name, res.detail))
		case res.held:
			out.Stderr(fmt.Sprintf("  ok    %-22s %s", p.name, res.detail))
		case p.want == mustBlock:
			holes++
			out.Stderr(fmt.Sprintf("  HOLE  %-22s %s", p.name, res.detail))
			out.Stderr(fmt.Sprintf("        why it matters: %s", p.why))
		default:
			broken++
			out.Stderr(fmt.Sprintf("  BROKE %-22s %s", p.name, res.detail))
			out.Stderr(fmt.Sprintf("        why it matters: %s", p.why))
		}
	}

	out.Stderr("")
	switch {
	case holes > 0:
		// Fail the command, not just the output: an audit that reports a
		// hole and still exits 0 will be wired into CI and ignored.
		return fmt.Errorf("%d sandbox hole(s) found — enforce mode does NOT fully contain egress on this host", holes)
	case broken > 0:
		return fmt.Errorf("%d legitimate capability broken by the profile", broken)
	}
	out.Stderr("PASS: no bypasses found — every known escape was refused by the OS.")
	out.Stderr("")
	out.Stderr("This audits the OS boundary only. To confirm traffic is also being")
	out.Stderr("governed, run the agent and check that its requests appear in Activity:")
	out.Stderr("  onecli run --enforce -- <agent>    then ask it to curl an API")
	out.Stderr("A request that succeeds but never appears in Activity bypassed the gateway.")
	return nil
}

// reportNative explains the native path rather than auditing a profile
// that is not the one in force for this agent.
func (c *SandboxAuditCmd) reportNative(out *output.Writer, agentName string) error {
	out.Stderr(fmt.Sprintf("%s uses the NATIVE enforce path.", agentName))
	out.Stderr("")
	out.Stderr("Its own OS sandbox is the enforcement layer, configured by OneCLI via")
	out.Stderr("--settings with three properties enforcement depends on:")
	out.Stderr("  enabled=true                    sandbox is on")
	out.Stderr("  failIfUnavailable=true          missing deps abort instead of degrading")
	out.Stderr("  allowUnsandboxedCommands=false  the documented escape hatch is off")
	out.Stderr("")
	out.Stderr("That sandbox can't be driven from here, so audit it from inside a session:")
	out.Stderr(fmt.Sprintf("  onecli run --enforce -- %s", strings.ToLower(strings.Fields(agentName)[0])))
	out.Stderr("then ask the agent to run each of these. All must fail:")
	for _, p := range sandboxProbes() {
		if p.want != mustBlock {
			continue
		}
		out.Stderr("  " + strings.Join(p.argv, " "))
	}
	out.Stderr("")
	out.Stderr("Then ask it to curl a real API: that must succeed AND appear in Activity.")
	out.Stderr("Success without an Activity entry means it bypassed the gateway.")
	return nil
}
