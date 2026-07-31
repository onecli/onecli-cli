//go:build darwin

package main

// pf anchor management for transparent-redirect mode (macOS).
//
// The rules answer one question: how do you redirect LOCALLY-ORIGINATED
// traffic to a loopback listener? pf's `rdr` only applies to packets
// arriving on an interface's inbound path, so it never fires for a
// connection this machine originates. The working idiom is two rules:
//
//	rdr pass on lo0 inet proto tcp from any to any port 443 -> 127.0.0.1 port P
//	pass out route-to lo0 inet proto tcp from any to any port 443 group G keep state
//
// The `route-to lo0` bounces outbound packets onto the loopback INBOUND
// path, where the `rdr` then matches. Without it the rdr is inert — the
// silent-no-op shape this codebase has been bitten by before.
//
// Scoping is by GROUP, not by port or address, and that is the security
// property: only processes running under the dedicated sandbox GID are
// redirected. Everything else on the machine is untouched, so a bug here
// cannot silently capture the user's own browser traffic.
//
// Everything lives in a NAMED anchor. Loading and flushing an anchor leaves
// the main ruleset alone, so we never clobber a user's firewall — a real
// risk given `pfctl -f` on the main ruleset replaces it wholesale.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	// pfAnchorName is the named anchor all OneCLI rules live under.
	//
	// It is nested under com.apple/ deliberately. macOS's stock ruleset
	// contains `anchor "com.apple/*"` and `rdr-anchor "com.apple/*"` but
	// nothing that would reach a top-level `onecli` anchor, so rules loaded
	// there are accepted by pfctl, visible in its output, and never
	// evaluated. Verified on a stock machine: `pfctl -s rules` lists only
	// the com.apple anchors.
	//
	// Nesting here means transparent redirect works with the system's own
	// ruleset untouched — no /etc/pf.conf edit, nothing to restore, and
	// nothing that a macOS update can revert out from under us.
	pfAnchorName = "com.apple/onecli"
	// pfctlPath is fixed, never resolved through PATH: a shadowed pfctl
	// must not become the enforcement layer (same reasoning as
	// sandboxExecPath).
	pfctlPath = "/sbin/pfctl"
	// sudoPath is likewise fixed. Reading and writing pf state needs root,
	// which setup grants through a sudoers entry scoped to pfctl alone.
	sudoPath = "/usr/bin/sudo"
)

// pfMaxGID is the largest GID pf accepts. INT_MAX is reserved as pf's
// "unknown gid" sentinel and is rejected by its parser — established by
// probing the real parser, not by reading the man page.
const pfMaxGID = 2147483646

// validateSandboxGID rejects GIDs that pf cannot express or that would
// scope redirection dangerously.
//
// GID 0 (wheel) is refused deliberately: scoping the redirect to it would
// capture root-owned traffic across the whole machine, far beyond the
// sandboxed agent. The whole point of group scoping is that a bug here
// cannot touch anything but the sandbox.
func validateSandboxGID(gid int) error {
	if gid <= 0 {
		return fmt.Errorf("sandbox GID must be a dedicated non-root group, got %d", gid)
	}
	if gid > pfMaxGID {
		return fmt.Errorf("sandbox GID %d exceeds the maximum pf accepts (%d)", gid, pfMaxGID)
	}
	return nil
}

// pfRules renders the anchor body for a forwarder port and sandbox GID.
// Returns an error rather than emitting rules that pf would reject at load
// time, when a partial failure is far more expensive to diagnose.
//
// Rule order matters and is not cosmetic. pf uses LAST-MATCH semantics for
// filter rules, so the default-deny is written first and the narrow allow
// second; reversing them would silently drop everything.
//
// The `block drop out ... group G` line is the security keystone. Because
// Seatbelt must now permit outbound 443 for pf to have a packet to redirect
// (measured: Seatbelt denies at connect(), before any packet exists, so a
// Seatbelt-denied connection never reaches pf), the OS-level deny that used
// to be our fail-closed guarantee is gone for that port. Without this line
// the design would be fail-OPEN: an anchor that is missing, flushed, or
// unreferenced would let the agent dial the internet directly and silently.
//
// With it, the anchor denies ALL egress for the sandbox group and then
// re-permits exactly one path: redirected 443. Loopback is allowed so the
// redirect itself, and local dev servers, keep working.
func pfRules(port uint16, gid int) (string, error) {
	if err := validateSandboxGID(gid); err != nil {
		return "", err
	}
	if port == 0 {
		return "", fmt.Errorf("forwarder port is unset")
	}
	var b strings.Builder
	// NAT: rewrite the redirected destination to our listener.
	fmt.Fprintf(&b,
		"rdr pass on lo0 inet proto tcp from any to any port %d -> 127.0.0.1 port %d\n",
		transparentRedirectPort, port)

	// Filter, in last-match order.
	// 1. Default-deny every protocol for the sandbox group.
	fmt.Fprintf(&b, "block drop out inet from any to any group %d\n", gid)
	fmt.Fprintf(&b, "block drop out inet6 from any to any group %d\n", gid)
	// 2. Loopback stays open: the redirect lands there, and local dev
	//    servers must keep working.
	fmt.Fprintf(&b, "pass out on lo0 inet from any to any group %d keep state\n", gid)
	// 3. The one sanctioned egress path: 443, diverted to the listener.
	fmt.Fprintf(&b,
		"pass out route-to lo0 inet proto tcp from any to any port %d group %d keep state\n",
		transparentRedirectPort, gid)
	// 4. DNS stays permitted: name resolution happens in the process
	//    itself, and without it the SNI we depend on is never produced.
	//    UDP/53 carries no payload we govern, and the gateway still
	//    adjudicates every connection that follows.
	fmt.Fprintf(&b, "pass out inet proto udp from any to any port 53 group %d keep state\n", gid)
	return b.String(), nil
}

// pfSudoWorks reports whether we can actually run pfctl under sudo without
// a password.
//
// Probes the capability rather than reading /etc/sudoers.d: that file is
// mode 440 root:wheel and unreadable by the user, so a read-based check
// reports MISSING even when the entry is present and working. Checking what
// we can DO is both accurate and the thing we care about.
func pfSudoWorks() bool {
	return pfctlCommand("-s", "info").Run() == nil
}

// pfAvailable reports whether pfctl exists and is usable.
func pfAvailable() error {
	if _, err := os.Stat(pfctlPath); err != nil {
		return fmt.Errorf("%s not found — cannot install transparent redirect", pfctlPath)
	}
	return nil
}

// pfctlCommand builds a privileged pfctl invocation.
//
// Every pf operation, including reads, needs root: /dev/pf is root-only.
// Setup grants this through a sudoers entry scoped to pfctl alone, so this
// is not blanket privilege.
//
// -n (non-interactive) is deliberate: if the sudoers entry is missing we
// want an immediate, diagnosable failure rather than a password prompt
// blocking a background process forever.
func pfctlCommand(args ...string) *exec.Cmd {
	full := append([]string{"-n", pfctlPath}, args...)
	return exec.Command(sudoPath, full...)
}

// pfValidateRules syntax-checks a ruleset WITHOUT root and WITHOUT loading
// it. Called before any privileged operation so a malformed rule surfaces
// as a clear error rather than a half-applied firewall change.
func pfValidateRules(rules string) error {
	cmd := exec.Command(pfctlPath, "-n", "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pf rules rejected: %s", pfCleanOutput(out))
	}
	return nil
}

// pfCleanOutput strips pfctl's unconditional main-ruleset warning so real
// errors are readable.
func pfCleanOutput(b []byte) string {
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(l)
		if t == "" ||
			strings.Contains(t, "could result in flushing") ||
			strings.Contains(t, "present in the main ruleset") ||
			strings.Contains(t, "See /etc/pf.conf") {
			continue
		}
		keep = append(keep, t)
	}
	return strings.Join(keep, "; ")
}

// pfEnabled reports whether the packet filter is currently active. Loading
// an anchor into a disabled pf silently does nothing — the exact silent-no-op
// failure mode this codebase guards against elsewhere, so the caller must
// check rather than assume.
func pfEnabled() (bool, error) {
	out, err := pfctlCommand("-s", "info").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("querying pf status: %s", pfCleanOutput(out))
	}
	return strings.Contains(string(out), "Status: Enabled"), nil
}

// pfAnchorLoaded returns the rules currently in our anchor, so callers can
// verify what is actually installed instead of trusting a prior write.
func pfAnchorLoaded() (string, error) {
	nat, err := pfctlStdout("-a", pfAnchorName, "-s", "nat")
	if err != nil {
		return "", fmt.Errorf("reading anchor: %w", err)
	}
	rules, err := pfctlStdout("-a", pfAnchorName, "-s", "rules")
	if err != nil {
		return "", fmt.Errorf("reading anchor rules: %w", err)
	}
	return nat + rules, nil
}

// pfctlStdout runs pfctl and returns STDOUT ONLY.
//
// This distinction is load-bearing, not tidiness. pfctl unconditionally
// writes to stderr on this kernel:
//
//	No ALTQ support in kernel
//	ALTQ related functions disabled
//
// CombinedOutput() folds those two lines into the result, so an EMPTY
// anchor read back as four nonblank lines. Any caller counting rules, or
// checking whether the anchor holds anything, silently saw a populated
// anchor when it was empty — including verifyLoaded, whose entire job is
// catching that case.
func pfctlStdout(args ...string) (string, error) {
	full := append([]string{"-n", pfctlPath}, args...)
	cmd := exec.Command(sudoPath, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s", pfCleanOutput(stderr.Bytes()))
	}
	return stdout.String(), nil
}

// pfEnable turns the packet filter on and returns the reference token
// macOS hands back.
//
// pf on macOS is REFERENCE COUNTED: `pfctl -E` increments a counter and
// returns a token, and pf stays up only while at least one reference is
// held. Observed live: pf was enabled during setup, then found Disabled
// later in the same session, which made an enforced run refuse to start
// with "pf is disabled". Enabling it once at setup is therefore not
// durable, so an enforced run enables it for itself.
//
// The token is deliberately NOT released on exit. Releasing it would turn
// pf off for anything else relying on it, and leaving it held is the
// conservative choice: pf up with an empty anchor governs nothing, while pf
// down would silently ungovern a concurrent enforced run.
func pfEnable() error {
	out, err := pfctlCommand("-E").CombinedOutput()
	if err != nil {
		return fmt.Errorf("enabling pf: %s", pfCleanOutput(out))
	}
	return nil
}

// pfEnsureEnabled turns pf on if it is off, so a run never fails merely
// because a reference elsewhere was dropped.
func pfEnsureEnabled() error {
	on, err := pfEnabled()
	if err != nil {
		return err
	}
	if on {
		return nil
	}
	if err := pfEnable(); err != nil {
		return err
	}
	// Verify rather than trust: `pfctl -E` can report success while pf
	// stays down if another reference was released concurrently.
	on, err = pfEnabled()
	if err != nil {
		return err
	}
	if !on {
		return fmt.Errorf("pf did not stay enabled; its rules would never evaluate")
	}
	return nil
}

// pfLoadAnchor installs the rules into the named anchor. Requires root.
//
// Returns a descriptive error on failure rather than degrading silently:
// --enforce promises enforcement, and a transparent redirect that did not
// install means traffic the caller believes is governed is not.
func pfLoadAnchor(rules string) error {
	if err := pfValidateRules(rules); err != nil {
		return err
	}
	cmd := pfctlCommand("-a", pfAnchorName, "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("loading pf anchor (needs root): %s", pfCleanOutput(out))
	}
	return nil
}

// pfFlushAnchor removes our rules, leaving the rest of pf untouched.
func pfFlushAnchor() error {
	out, err := pfctlCommand("-a", pfAnchorName, "-F", "all").CombinedOutput()
	if err != nil {
		return fmt.Errorf("flushing pf anchor: %s", pfCleanOutput(out))
	}
	return nil
}

// pfMainRulesetReferencesAnchor reports whether the system ruleset actually
// dispatches into our anchor. An anchor that nothing references holds rules
// that never evaluate — loaded, visible in pfctl output, and completely
// inert. Checking is the difference between "installed" and "working".
func pfMainRulesetReferencesAnchor() (bool, error) {
	natOut, err := pfctlStdout("-s", "nat")
	if err != nil {
		return false, fmt.Errorf("reading main nat ruleset: %w", err)
	}
	ruleOut, err := pfctlStdout("-s", "rules")
	if err != nil {
		return false, fmt.Errorf("reading main ruleset: %w", err)
	}
	combined := natOut + ruleOut
	return anchorIsReachable(combined, pfAnchorName), nil
}

// anchorIsReachable reports whether a main ruleset dispatches into the
// named anchor.
//
// Nesting is the subtle part. Our anchor is "com.apple/onecli", and macOS's
// stock ruleset contains `anchor "com.apple/*"`. The wildcard covers every
// child, so an exact-name search would wrongly report the anchor as
// unreachable and refuse to run — which is exactly what happened before
// this function existed. A parent wildcard is a real reference and must
// count as one.
func anchorIsReachable(ruleset, anchor string) bool {
	if strings.Contains(ruleset, `"`+anchor+`"`) {
		return true
	}
	// Walk parent prefixes: com.apple/onecli -> com.apple/*, then */*.
	parts := strings.Split(anchor, "/")
	for i := len(parts) - 1; i > 0; i-- {
		prefix := strings.Join(parts[:i], "/")
		if strings.Contains(ruleset, `"`+prefix+`/*"`) {
			return true
		}
	}
	return strings.Contains(ruleset, `anchor "*"`)
}

// sandboxGID resolves the dedicated group used to scope redirection.
// Returns the numeric GID.
func sandboxGID(groupName string) (int, error) {
	out, err := exec.Command("/usr/bin/dscl", ".", "-read",
		"/Groups/"+groupName, "PrimaryGroupID").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("group %q not found", groupName)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected dscl output for group %q", groupName)
	}
	gid, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return 0, fmt.Errorf("parsing GID for %q: %w", groupName, err)
	}
	return gid, nil
}
