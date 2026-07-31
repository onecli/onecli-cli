//go:build darwin

package main

// Tests for the `onecli sandbox transparent` commands and the anchor
// reachability logic behind them.
//
// This file replaces an earlier scaffolding test whose only job was to
// PRINT the setup script, because no CLI command existed to do it. That
// gap (setup reachable only from tests, and an error message naming a
// command that did not exist) was found by auditing which functions had
// non-test callers, and is what these commands fix.

import (
	"strings"
	"testing"
)

// TestAnchorIsReachableUnderstandsNesting is the regression guard for a bug
// that cost real debugging time: our anchor is "com.apple/onecli" and the
// stock macOS ruleset only contains `anchor "com.apple/*"`. An exact-name
// search reported the anchor as unreachable and refused to run, even though
// the wildcard covers it.
func TestAnchorIsReachableUnderstandsNesting(t *testing.T) {
	stockRuleset := `scrub-anchor "com.apple/*" all fragment reassemble
anchor "com.apple/*" all
nat-anchor "com.apple/*" all
rdr-anchor "com.apple/*" all`

	for name, tc := range map[string]struct {
		ruleset string
		anchor  string
		want    bool
	}{
		"parent wildcard covers a nested anchor": {stockRuleset, "com.apple/onecli", true},
		"exact name":                             {`anchor "onecli" all`, "onecli", true},
		"global wildcard":                        {`anchor "*" all`, "onecli", true},
		"top-level anchor NOT covered":           {stockRuleset, "onecli", false},
		"unrelated wildcard":                     {`anchor "com.other/*" all`, "com.apple/onecli", false},
		"empty ruleset":                          {"", "com.apple/onecli", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := anchorIsReachable(tc.ruleset, tc.anchor); got != tc.want {
				t.Fatalf("anchorIsReachable(%q) = %v, want %v", tc.anchor, got, tc.want)
			}
		})
	}
}

// TestTransparentSetupCommandNamesRealCommands: the setup output tells the
// operator what to run next, and every command it names must exist. The
// previous error message pointed at `onecli enforce setup-transparent`,
// which was never implemented.
func TestTransparentSetupCommandNamesRealCommands(t *testing.T) {
	err := verifyTransparentSetup()
	if err == nil {
		t.Skip("machine is set up; the guidance path is not exercised")
	}
	if strings.Contains(err.Error(), "onecli enforce") {
		t.Fatalf("error names a command that does not exist: %v", err)
	}
}

// TestHelperInstallHintIsComplete: the hint is the only place the setgid
// helper's install is documented in the product, so it must carry every
// step. A missing chmod would leave a binary without the setgid bit, which
// fails closed but leaves the user stuck.
func TestHelperInstallHintIsComplete(t *testing.T) {
	hint := helperInstallHint("")
	for _, required := range []string{
		"cc ", "-DONECLI_SANDBOX_GID=", "chown root:", "chmod 2755",
		setgidHelperPath, transparentGroupName,
	} {
		if !strings.Contains(hint, required) {
			t.Fatalf("install hint is missing %q:\n%s", required, hint)
		}
	}
}

// TestTransparentStatusCommandRuns exercises the status path end to end.
// It must never error, whatever the machine's state: a status command that
// fails when things are unconfigured is useless precisely when it is needed.
func TestTransparentStatusCommandRuns(t *testing.T) {
	cmd := &SandboxTransparentStatusCmd{}
	if err := cmd.Run(); err != nil {
		t.Fatalf("status command failed: %v", err)
	}
}
