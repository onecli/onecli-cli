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

func launcherPath() string { return "" }

func profile(uint16) string { return "" }

func wrapArgv(_, binary string, args []string) []string {
	// Unreachable in practice: callers gate on Available() first. Return a
	// direct invocation so a misuse runs the command rather than a broken
	// argv (unconfined — hence the Available() gate).
	return append([]string{binary}, args...)
}
