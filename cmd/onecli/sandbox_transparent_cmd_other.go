//go:build !darwin

package main

// Transparent redirect is macOS-only (it depends on pf). These stubs keep
// the command tree identical across platforms so help output and tests do
// not diverge; both subcommands explain why they are unavailable rather
// than silently missing.

import (
	"fmt"
	"runtime"
)

// SandboxTransparentCmd groups the transparent-redirect subcommands.
type SandboxTransparentCmd struct {
	Status SandboxTransparentStatusCmd `cmd:"" help:"Report whether transparent redirect is ready to use."`
	Setup  SandboxTransparentSetupCmd  `cmd:"" help:"Print the one-time privileged setup script (does not run it)."`
}

// SandboxTransparentStatusCmd reports readiness.
type SandboxTransparentStatusCmd struct{}

// Run explains that the platform is unsupported.
func (c *SandboxTransparentStatusCmd) Run() error {
	return fmt.Errorf("transparent redirect requires macOS pf; this is %s", runtime.GOOS)
}

// SandboxTransparentSetupCmd prints the privileged setup script.
type SandboxTransparentSetupCmd struct {
	Helper string `help:"Path to the setgid helper source." default:""`
}

// Run explains that the platform is unsupported.
func (c *SandboxTransparentSetupCmd) Run() error {
	return fmt.Errorf("transparent redirect requires macOS pf; this is %s", runtime.GOOS)
}
