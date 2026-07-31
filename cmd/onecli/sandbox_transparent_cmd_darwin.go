//go:build darwin

package main

// `onecli sandbox transparent` — status and setup for transparent redirect.
//
// This is the user-facing entry point for the one privileged step. Before it
// existed, the readiness check told users to run a command that did not
// exist and setup only worked by hand-generating a script — a gap found by
// auditing which functions had non-test callers.
//
// Deliberately split into two subcommands:
//   status  reports readiness and needs no privilege
//   setup   PRINTS the script rather than executing it
//
// setup does not run itself under sudo on the user's behalf. It creates a
// group and edits sudoers, and a security tool that does that invisibly has
// not earned the trust it is asking for. Printing the exact commands lets
// the operator read them first, and makes the change auditable and
// reversible.

import (
	"fmt"
	"os"
	"os/user"

	"github.com/onecli/onecli-cli/pkg/output"
)

// SandboxTransparentCmd groups the transparent-redirect subcommands.
type SandboxTransparentCmd struct {
	Status SandboxTransparentStatusCmd `cmd:"" help:"Report whether transparent redirect is ready to use."`
	Setup  SandboxTransparentSetupCmd  `cmd:"" help:"Print the one-time privileged setup script (does not run it)."`
}

// SandboxTransparentStatusCmd reports readiness.
type SandboxTransparentStatusCmd struct{}

// Run prints the readiness report.
func (c *SandboxTransparentStatusCmd) Run() error {
	printTransparentStatus(os.Stdout)
	return nil
}

// SandboxTransparentSetupCmd prints the privileged setup script.
type SandboxTransparentSetupCmd struct {
	Helper string `help:"Path to the setgid helper source (defaults to the copy shipped with this checkout)." default:""`
}

// Run renders the setup script for the operator to review and execute.
func (c *SandboxTransparentSetupCmd) Run() error {
	out := output.New()

	if err := verifyTransparentSetup(); err == nil {
		out.Stderr("onecli: transparent redirect is already set up. " +
			"Run `onecli sandbox transparent status` to confirm.")
		return nil
	}

	gid, err := sandboxGID(transparentGroupName)
	if err != nil {
		gid, err = nextFreeGID()
		if err != nil {
			return fmt.Errorf("choosing a group ID: %w", err)
		}
	}
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolving the current user: %w", err)
	}

	fmt.Print(transparentSetupScript(gid, u.Username))
	fmt.Println()
	fmt.Println("# Review the above, then run it:")
	fmt.Printf("#   onecli sandbox transparent setup | sudo bash\n")
	fmt.Println("#")
	fmt.Println("# Then install the setgid helper (needed because pf matches the")
	fmt.Println("# EFFECTIVE gid, which an unprivileged process cannot adopt):")
	fmt.Printf("#   %s\n", helperInstallHint(c.Helper))
	return nil
}

// helperInstallHint renders the compile-and-install line for the setgid
// helper, which cannot live in the shell script above because it needs the
// C source path from this checkout.
func helperInstallHint(srcOverride string) string {
	src := srcOverride
	if src == "" {
		src = "internal/sandbox/helper/onecli-sandbox-gid.c"
	}
	return fmt.Sprintf(
		"sudo sh -c 'cc -O2 -Wall -Wextra -Werror -DONECLI_SANDBOX_GID=$(dscl . -read /Groups/%s PrimaryGroupID | awk \"{print \\$2}\") "+
			"-o %s %s && chown root:%s %s && chmod 2755 %s'",
		transparentGroupName, setgidHelperPath, src,
		transparentGroupName, setgidHelperPath, setgidHelperPath)
}
