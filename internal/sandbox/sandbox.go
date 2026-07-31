// Package sandbox provides the OS-level network sandbox for
// `onecli run --enforce`: it confines a process tree so its only egress
// path is a loopback forwarder fronting the OneCLI gateway. Direct dials
// fail at the OS, even if the process strips its proxy env.
//
// The mechanism is platform-specific (macOS Seatbelt today; Linux
// bubblewrap + network namespace planned), selected at compile time via
// build tags: the real implementation lives in sandbox_<goos>.go, and
// sandbox_other.go fails closed everywhere else. Non-OS-specific concerns
// (the forwarder, proxy-env rewriting, per-agent quirks, the audit's
// probe matrix) stay in the caller.
package sandbox

// Available reports whether enforce-wrap is supported on this host, with a
// user-facing reason when it isn't. Callers must check this before
// promising enforcement, and fail closed on error.
func Available() error { return available() }

// Materialize writes the sandbox profile an enforced run applies and
// returns a handle to it (on macOS, the path to the Seatbelt profile
// under ~/.onecli). It returns the REAL artifact a run uses, so a wrap or
// audit operates on the shipped policy rather than a drifted copy.
func Materialize(forwarderPort uint16) (string, error) { return materialize(forwarderPort) }

// LauncherPath is the OS launcher that applies the sandbox to a command
// (on macOS, /usr/bin/sandbox-exec). A fixed path, not a PATH lookup: a
// shadowed launcher must never become the enforcement layer.
func LauncherPath() string { return launcherPath() }

// WrapArgv builds the argv passed to LauncherPath that runs binary+args
// confined by the materialized profile. args already include any
// per-agent quirk flags the caller appended.
func WrapArgv(profile, binary string, args []string) []string {
	return wrapArgv(profile, binary, args)
}

// Profile returns the sandbox profile text. Exposed so the audit can
// derive a deliberately-holed variant to prove the audit itself detects
// holes; normal runs use Materialize.
func Profile(forwarderPort uint16) string { return profile(forwarderPort) }

// MaterializeOpts writes the profile for an explicit network posture.
// Prefer Materialize unless you are enabling transparent redirect, which
// requires a verified pf anchor to stay fail-closed.
func MaterializeOpts(opts Options) (string, error) { return materializeOpts(opts) }

// ProfileOpts returns the profile text for an explicit posture.
func ProfileOpts(opts Options) string { return profileOpts(opts) }
