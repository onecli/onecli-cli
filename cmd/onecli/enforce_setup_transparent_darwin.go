//go:build darwin

package main

// Setup and readiness for transparent redirect, surfaced by
// `onecli sandbox transparent {status,setup}`.
//
// Transparent redirect needs root exactly twice, both one-time:
//   1. create the dedicated group that scopes redirection
//   2. authorize loading the pf anchor at session start
//
// Neither happens in the data path. After setup, enforced runs install and
// flush the anchor through a narrowly-scoped sudoers rule that permits
// pfctl and nothing else.
//
// This command is deliberately explicit and reversible: it prints exactly
// what it will do, what it changed, and how to undo it. A security product
// that quietly edits sudoers has no business asking to be trusted.

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	// transparentGroupName is the dedicated group whose egress pf captures.
	transparentGroupName = "onecli-sandbox"
	// transparentSudoersPath scopes the standing privilege to pfctl alone.
	transparentSudoersPath = "/etc/sudoers.d/onecli-transparent"
	// transparentGIDFloor keeps us clear of system groups.
	transparentGIDFloor = 700
)

// transparentSetupPlan describes what setup will change, so it can be shown
// before anything is touched.
type transparentSetupPlan struct {
	GroupExists  bool
	GroupGID     int
	SudoersOK    bool
	PFEnabled    bool
	AnchorInMain bool
}

// inspectTransparentSetup reports current state WITHOUT changing anything.
func inspectTransparentSetup() transparentSetupPlan {
	var p transparentSetupPlan
	if gid, err := sandboxGID(transparentGroupName); err == nil {
		p.GroupExists = true
		p.GroupGID = gid
	}
	// Capability probe, not a file read: the sudoers file is mode 440
	// root:wheel, so reading it fails even when the entry works.
	p.SudoersOK = pfSudoWorks()
	if on, err := pfEnabled(); err == nil {
		p.PFEnabled = on
	}
	if ref, err := pfMainRulesetReferencesAnchor(); err == nil {
		p.AnchorInMain = ref
	}
	return p
}

// nextFreeGID finds an unused GID at or above the floor, so setup never
// collides with an existing group.
func nextFreeGID() (int, error) {
	out, err := exec.Command("/usr/bin/dscl", ".", "-list", "/Groups", "PrimaryGroupID").Output()
	if err != nil {
		return 0, fmt.Errorf("listing groups: %w", err)
	}
	used := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if gid, err := strconv.Atoi(f[len(f)-1]); err == nil {
			used[gid] = true
		}
	}
	for gid := transparentGIDFloor; gid < transparentGIDFloor+500; gid++ {
		if !used[gid] {
			return gid, nil
		}
	}
	return 0, fmt.Errorf("no free GID found above %d", transparentGIDFloor)
}

// transparentSetupScript renders the shell the user runs under sudo.
//
// Returned as text for the user to inspect and execute rather than executed
// for them: this edits sudoers and creates a group, and a security tool
// should show its work. It is idempotent and safe to re-run.
func transparentSetupScript(gid int, user string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# OneCLI transparent-redirect setup. Idempotent; safe to re-run.\n")
	b.WriteString("set -euo pipefail\n\n")

	b.WriteString("# 1. Dedicated group that scopes pf redirection.\n")
	fmt.Fprintf(&b, "if ! dscl . -read /Groups/%s >/dev/null 2>&1; then\n", transparentGroupName)
	fmt.Fprintf(&b, "  dscl . -create /Groups/%s\n", transparentGroupName)
	fmt.Fprintf(&b, "  dscl . -create /Groups/%s PrimaryGroupID %d\n", transparentGroupName, gid)
	fmt.Fprintf(&b, "  dscl . -create /Groups/%s RealName 'OneCLI sandboxed agents'\n", transparentGroupName)
	fmt.Fprintf(&b, "  echo 'created group %s (gid %d)'\n", transparentGroupName, gid)
	b.WriteString("else\n  echo 'group already exists'\nfi\n\n")

	fmt.Fprintf(&b, "# 2. Add %s to the group so enforced runs can adopt it.\n", user)
	fmt.Fprintf(&b, "dseditgroup -o edit -a %s -t user %s 2>/dev/null || true\n\n", user, transparentGroupName)

	b.WriteString("# 3. Scoped sudo for pfctl ONLY. No blanket root.\n")
	fmt.Fprintf(&b, "cat > %s <<'SUDOERS'\n", transparentSudoersPath)
	fmt.Fprintf(&b, "%s ALL=(root) NOPASSWD: %s\n", user, pfctlPath)
	b.WriteString("SUDOERS\n")
	fmt.Fprintf(&b, "chmod 440 %s\n", transparentSudoersPath)
	fmt.Fprintf(&b, "visudo -cf %s\n\n", transparentSudoersPath)

	b.WriteString("# 4. pf must be enabled for any anchor to evaluate.\n")
	fmt.Fprintf(&b, "%s -E >/dev/null 2>&1 || true\n\n", pfctlPath)

	b.WriteString("echo\necho 'OneCLI transparent redirect is set up.'\n")
	fmt.Fprintf(&b, "echo 'To undo: sudo rm %s && sudo dscl . -delete /Groups/%s'\n",
		transparentSudoersPath, transparentGroupName)
	return b.String()
}

// verifyTransparentSetup checks every precondition and returns a specific
// reason for the first failure. Callers must treat any error as fatal:
// transparent mode widens the Seatbelt profile, so running it without a
// working anchor would be ungoverned direct egress.
func verifyTransparentSetup() error {
	if err := pfAvailable(); err != nil {
		return err
	}
	gid, err := sandboxGID(transparentGroupName)
	if err != nil {
		return fmt.Errorf("group %q missing — run `onecli sandbox transparent setup`",
			transparentGroupName)
	}
	if err := validateSandboxGID(gid); err != nil {
		return err
	}
	on, err := pfEnabled()
	if err != nil {
		return fmt.Errorf("cannot determine pf status: %w", err)
	}
	if !on {
		return fmt.Errorf("pf is disabled; the anchor would load but never evaluate " +
			"(enable with `sudo pfctl -E`)")
	}
	ref, err := pfMainRulesetReferencesAnchor()
	if err != nil {
		return fmt.Errorf("cannot read the main pf ruleset: %w", err)
	}
	if !ref {
		return fmt.Errorf("the main pf ruleset does not reference the %q anchor, "+
			"so its rules would never evaluate", pfAnchorName)
	}
	return nil
}

// printTransparentStatus renders a human-readable readiness report.
func printTransparentStatus(w *os.File) {
	p := inspectTransparentSetup()
	mark := func(ok bool) string {
		if ok {
			return "ok  "
		}
		return "MISSING"
	}
	fmt.Fprintf(w, "OneCLI transparent redirect\n\n")
	fmt.Fprintf(w, "  %s group %s", mark(p.GroupExists), transparentGroupName)
	if p.GroupExists {
		fmt.Fprintf(w, " (gid %d)", p.GroupGID)
	}
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "  %s scoped sudo for pfctl (verified by running it)\n", mark(p.SudoersOK))
	fmt.Fprintf(w, "  %s pf enabled\n", mark(p.PFEnabled))
	fmt.Fprintf(w, "  %s main ruleset references the %q anchor\n", mark(p.AnchorInMain), pfAnchorName)
	fmt.Fprintf(w, "\n")
	if err := verifyTransparentSetup(); err != nil {
		fmt.Fprintf(w, "  NOT READY: %v\n", err)
		return
	}
	fmt.Fprintf(w, "  READY — enforced runs can use transparent redirect.\n")
}
