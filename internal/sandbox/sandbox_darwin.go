//go:build darwin

package sandbox

// macOS backend: sandbox-exec with a generated Seatbelt profile that
// denies network-outbound except loopback. Seatbelt is inherited by the
// whole process tree and cannot be shed, so the only egress path is the
// loopback forwarder fronting the gateway.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// seatbeltProfileTemplate is the Seatbelt profile for enforce-wrap mode.
// The forwarder port doesn't appear because ALL loopback is allowed.
//
// Design notes, validated empirically:
//   - Network governance only. Filesystem policy is deliberately absent:
//     WHICH hosts an agent may reach is the gateway's policy decision,
//     and file confinement is the agent's own sandbox's job (kept where
//     it can nest, disabled via the quirks table where it can't).
//   - Unix sockets stay allowed — they are local IPC (DNS via
//     mDNSResponder, ssh-agent), not egress. Except the Docker daemon:
//     a container runs OUTSIDE this sandbox, so docker.sock would be a
//     one-line egress bypass (`docker run --net=host curl ...`). The deny
//     is a REGEX on the socket name, not a path-literal on /var/run:
//     Seatbelt matches the RESOLVED path, and Docker Desktop ships
//     /var/run/docker.sock as a symlink into ~/.docker/run, so literal
//     rules for the well-known path silently never fire. Verified by
//     TestLiveEnforceWrapDockerSocketDenied — with path-literal rules the
//     daemon answered /version from inside the sandbox.
//   - All loopback is allowed, not just the forwarder port: local dev
//     servers must keep working, and loopback traffic cannot leave the
//     machine on its own.
//   - lsopen is denied: LaunchServices `open` launches the target app
//     OUTSIDE the sandbox, which would be a URL-fetch escape. Apple
//     Events are denied for the same reason (and further TCC-gated by
//     the OS).
//
// The filesystem rules exist FOR the network guarantee, which is why a
// "network-only" profile was never actually network-only in effect. A
// process that cannot dial out today can still write a file that an
// UNSANDBOXED process executes tomorrow — a LaunchAgent, a line in
// ~/.zshrc, a binary on $PATH — and that process has unrestricted egress.
// Deferred execution is a bypass with a delay, so the surfaces granting
// it are denied. Every one was reachable until these rules existed;
// `onecli sandbox audit` keeps them honest.
//
// This is deliberately NOT a cwd-only jail (the shape Claude Code's own
// sandbox uses). Agents legitimately write across $HOME — tool caches,
// language toolchains, session state — and a profile that breaks ordinary
// work gets switched off, which protects nobody. Denying the handful of
// launch-on-boot and run-on-next-shell surfaces closes the escape while
// leaving normal work untouched.
//
// {{HOME}} is materialized by profileFor: Seatbelt performs no
// $HOME expansion, so an unexpanded placeholder would load cleanly and
// match nothing — the same silent-no-op shape as the docker.sock bug.
//
// {{PORT}} is the loopback forwarder's port, and scoping the allow to it
// is load-bearing, not tidiness. Seatbelt's `(remote ip "localhost:*")`
// does NOT mean 127.0.0.1: it means any address local to this machine,
// which includes the host's LAN interfaces. With a wildcard port that let
// a sandboxed agent reach ANY listener bound to the host — a Docker
// container publishing a port, a dev server on 0.0.0.0 — and any such
// listener is unsandboxed, so it relays to the internet. Demonstrated
// end-to-end: with proxy env stripped, a secret was exfiltrated to a
// container via the host's LAN IP while direct egress was blocked.
//
// Seatbelt cannot express "127.0.0.1 but not the LAN" (it rejects any
// host other than * or localhost), so the port is the discriminator. The
// forwarder binds 127.0.0.1 on a random ephemeral port chosen per run, so
// an attacker cannot pre-position a listener there, and the LAN interface
// on that port has nothing listening.
const seatbeltProfileTemplate = `(version 1)
; OneCLI enforce-wrap profile: OS-level network governance.
; The agent's only network path is the loopback auth forwarder fronting
; the OneCLI gateway. Filesystem rules are limited to the surfaces that
; would hand an unsandboxed process the network on the agent's behalf.
(allow default)
{{NETWORK}}
(deny lsopen)
(deny appleevent-send)

; Deferred egress: launchd runs these OUTSIDE the sandbox, at login/boot.
(deny file-write* (subpath "{{HOME}}/Library/LaunchAgents"))
(deny file-write* (subpath "/Library/LaunchAgents"))
(deny file-write* (subpath "/Library/LaunchDaemons"))

; Deferred egress: the user's next interactive shell is unsandboxed.
(deny file-write*
  (literal "{{HOME}}/.zshrc")
  (literal "{{HOME}}/.zshenv")
  (literal "{{HOME}}/.zprofile")
  (literal "{{HOME}}/.bashrc")
  (literal "{{HOME}}/.bash_profile")
  (literal "{{HOME}}/.profile"))

; Deferred egress: a dropped binary on $PATH runs unsandboxed later.
(deny file-write* (subpath "/usr/local/bin"))
(deny file-write* (subpath "/opt/homebrew/bin"))

; Deferred egress: git hooks run UNSANDBOXED on the user's next commit —
; the highest-frequency trigger in this class, and unremarkable coming
; from an agent whose job is editing repos. Path-relative (any .git
; anywhere) because the agent's repo isn't known when this is rendered.
;
; The *.sample carve-out exists because git COPIES the template hooks
; into every new .git/hooks on init and clone; denying them outright
; breaks 'git clone' entirely, which is far worse than the vector. The
; samples are inert: git only ever runs a hook at its exact name, so a
; file ending in .sample is never executed, and cp/mv of a sample onto a
; real hook name is itself denied by the rule above (verified).
;
; Scope limit, stated plainly: this denies the hooks DIRECTORY, not
; core.hooksPath in a repo's own .git/config. That config file cannot be
; denied — git writes it to create a repository, so the deny breaks
; 'git init' and 'git clone' outright. A sandboxed agent can therefore
; still point a repo's hooks at a directory it controls, and that hook
; WILL execute unsandboxed on the next commit (confirmed experimentally,
; not assumed). The GLOBAL gitconfig is denied below because it is
; writable without breaking git and would apply to every repo at once.
; The per-repo path stays open, and closing it needs a mechanism this
; profile doesn't have — most likely inspecting core.hooksPath at commit
; time rather than a filesystem rule.
(deny file-write* (regex #"/\.git/hooks/"))
(allow file-write* (regex #"/\.git/hooks/[^/]*\.sample$"))
(deny file-write* (literal "{{HOME}}/.gitconfig"))
(deny file-write* (regex #"/\.config/git/config$"))

; Self-protection: the sandbox profile and the Claude enforce settings
; are what make the NEXT run enforced. A sandboxed process that rewrites
; them disables enforcement on itself, so they are denied by name.
;
; Scoped to those two artifacts rather than all of ~/.onecli: the
; directory also holds config.json and credentials, which onecli's own
; commands legitimately write. Denying the whole subpath broke
; 'onecli config set' and 'onecli auth login' from inside an enforced
; session, with a raw "operation not permitted" — a real regression that
; the audit's own probes missed because they only tested writes to a
; made-up path under the directory, never the paths the product uses.
; Reads stay allowed throughout; the agent needs its own config.
(deny file-write* (literal "{{HOME}}/.onecli/enforce-wrap.sb"))
(deny file-write* (literal "{{HOME}}/.onecli/enforce-sandbox-settings.json"))

; Credential staging: the payload an exfil attempt is after. Egress is
; already governed, but denying the read removes the reward for finding a
; channel we missed.
(deny file-read* (subpath "{{HOME}}/.ssh"))
(deny file-read* (subpath "{{HOME}}/.aws"))
`

// transparentNetworkStanza replaces the loopback-only network rules when
// transparent redirect is active.
//
// Why this exists, and why it is NOT a weakening: Seatbelt adjudicates at
// connect(), in the socket layer, BEFORE any packet is emitted. Measured
// against unroutable addresses, a Seatbelt-denied connect returns EPERM in
// 16ms while an allowed one takes the full 6s network timeout. pf works on
// packets, so it can only redirect a connection Seatbelt permitted. To
// transparently proxy an app that dials directly — the Cursor extension
// host being the case that forced this — the profile must let the packet
// out so pf can divert it to the loopback listener.
//
// The governance that Seatbelt used to provide for port 443 moves to the pf
// anchor, which default-denies the sandbox group and re-permits only
// loopback, redirected 443, and DNS. Enforcement is therefore preserved,
// not traded away, but it now depends on the anchor being loaded — which is
// why enabling this mode REQUIRES a verified anchor (see
// requireTransparentAnchor in the caller) and refuses to run without one.
//
// Ports other than 443 stay denied at the Seatbelt layer, so this widens
// exactly the one port pf is configured to capture.
const transparentNetworkStanza = `(deny network-outbound)
(allow network-outbound (remote unix-socket))
(deny network-outbound (regex #"docker\.sock$"))
(allow network-outbound (remote tcp "localhost:{{PORT}}"))
; Transparent redirect: pf diverts this to the loopback listener. Seatbelt
; must permit the connect() for a packet to exist for pf to act on.
(allow network-outbound (remote tcp "*:443"))`

// loopbackNetworkStanza is the default: no direct egress at any port.
const loopbackNetworkStanza = `(deny network-outbound)
(allow network-outbound (remote unix-socket))
(deny network-outbound (regex #"docker\.sock$"))
(allow network-outbound (remote tcp "localhost:{{PORT}}"))`

// Options selects the network posture of the rendered profile.
type Options struct {
	// ForwarderPort is the loopback listener the sandbox may reach.
	ForwarderPort uint16
	// Transparent permits outbound 443 so a pf anchor can redirect it.
	// Only set this when a verified anchor is loaded: without one, the
	// permitted port becomes ungoverned direct egress.
	Transparent bool
}

// profileFor renders the profile for a home directory. Exported through
// Profile()/Materialize() so the audit and the enforced run share one
// definition — two copies of a security policy is how the docker.sock
// rule stayed broken while a text assertion passed.
func profileFor(home string, forwarderPort uint16) string {
	return profileForOpts(home, Options{ForwarderPort: forwarderPort})
}

func profileForOpts(home string, opts Options) string {
	network := loopbackNetworkStanza
	if opts.Transparent {
		network = transparentNetworkStanza
	}
	out := strings.Replace(seatbeltProfileTemplate, "{{NETWORK}}", network, 1)
	out = strings.ReplaceAll(out, "{{HOME}}", home)
	return strings.ReplaceAll(out, "{{PORT}}", strconv.Itoa(int(opts.ForwarderPort)))
}

// sandboxExecPath is where macOS ships sandbox-exec. A fixed path, not
// LookPath: a PATH-shadowed sandbox-exec must never become the
// enforcement layer.
const sandboxExecPath = "/usr/bin/sandbox-exec"

func available() error {
	if _, err := os.Stat(sandboxExecPath); err != nil {
		return fmt.Errorf("%s not found — cannot apply the OS sandbox", sandboxExecPath)
	}
	return nil
}

func launcherPath() string { return sandboxExecPath }

// profile returns the rendered profile for the current user. Empty home
// is not silently tolerated: an unrendered {{HOME}} would load cleanly
// and match nothing, which is the silent-no-op failure this package
// exists to avoid.
func profile(forwarderPort uint16) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return profileFor(home, forwarderPort)
}

func profileOpts(opts Options) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return profileForOpts(home, opts)
}

// materialize writes the Seatbelt profile under ~/.onecli and returns its
// path, rewriting only when stale.
func materialize(forwarderPort uint16) (string, error) {
	return materializeOpts(Options{ForwarderPort: forwarderPort})
}

func materializeOpts(opts Options) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	dir := filepath.Join(home, ".onecli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating profile dir: %w", err)
	}
	rendered := profileForOpts(home, opts)
	// Distinct filenames per mode: a transparent profile permits outbound
	// 443 and is only safe with a loaded anchor, so it must never be
	// picked up by a normal run through a stale shared path.
	name := "enforce-wrap.sb"
	if opts.Transparent {
		name = "enforce-wrap-transparent.sb"
	}
	path := filepath.Join(dir, name)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == rendered {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
		return "", fmt.Errorf("writing sandbox profile: %w", err)
	}
	return path, nil
}

// wrapArgv builds the sandbox-exec argv: sandbox-exec -f <profile> <binary> <args...>.
func wrapArgv(profilePath, binary string, args []string) []string {
	return append([]string{"sandbox-exec", "-f", profilePath, binary}, args...)
}
