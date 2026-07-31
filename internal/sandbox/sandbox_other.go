//go:build !darwin

package sandbox

// Fallback backend for platforms without an implemented sandbox
// (currently everything but macOS). Linux (bubblewrap + network
// namespace) and Windows are planned; until then enforce-wrap fails
// closed with a clear reason rather than silently running ungoverned.

import (
	"fmt"
	"runtime"
)

func available() error {
	return fmt.Errorf("--enforce for this agent requires the OneCLI sandbox, which supports macOS only for now (got %s)", runtime.GOOS)
}

func materialize(uint16) (string, error) { return "", available() }

// Options mirrors the darwin type so callers compile everywhere. Transparent
// redirect is macOS-only today (it depends on pf); the Linux equivalent is
// an nftables REDIRECT inside the network namespace.
type Options struct {
	ForwarderPort uint16
	Transparent   bool
}

func materializeOpts(Options) (string, error) { return "", available() }

func profileOpts(Options) string { return "" }

func launcherPath() string { return "" }

func profile(uint16) string { return "" }

func wrapArgv(_, binary string, args []string) []string {
	// Unreachable in practice: callers gate on Available() first. Return a
	// direct invocation so a misuse runs the command rather than a broken
	// argv (unconfined — hence the Available() gate).
	return append([]string{binary}, args...)
}
