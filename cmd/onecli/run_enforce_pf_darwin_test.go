//go:build darwin

package main

// Tests for the pf anchor layer. Everything here runs WITHOUT root: rule
// rendering and syntax validation are the parts that must be right before
// any privileged operation happens, and `pfctl -n` validates syntax
// unprivileged.

import (
	"strings"
	"testing"
)

// TestPFRulesAreValidSyntax is the load-bearing test: it feeds our generated
// rules to the real pfctl parser. A hand-written assertion on the string
// would only prove the rules match my expectations, not that the kernel's
// parser accepts them — the same class of mistake as a Seatbelt rule that
// loads cleanly and matches nothing.
func TestPFRulesAreValidSyntax(t *testing.T) {
	if err := pfAvailable(); err != nil {
		t.Skipf("pfctl unavailable: %v", err)
	}
	for _, tc := range []struct {
		port uint16
		gid  int
	}{
		{8080, 5000},
		{1, 1},
		{65535, pfMaxGID},
		{443, 20},
	} {
		rules, err := pfRules(tc.port, tc.gid)
		if err != nil {
			t.Fatalf("pfRules(%d, %d): %v", tc.port, tc.gid, err)
		}
		if err := pfValidateRules(rules); err != nil {
			t.Fatalf("pfRules(%d, %d) produced invalid syntax: %v\n%s",
				tc.port, tc.gid, err, rules)
		}
	}
}

// TestPFValidateRulesRejectsGarbage proves the validator actually validates.
// Without this, TestPFRulesAreValidSyntax could pass against a checker that
// accepts anything.
func TestPFValidateRulesRejectsGarbage(t *testing.T) {
	if err := pfAvailable(); err != nil {
		t.Skipf("pfctl unavailable: %v", err)
	}
	for name, rules := range map[string]string{
		"prose":           "this is not a pf rule\n",
		"bad keyword":     "rdrr pass on lo0 -> 127.0.0.1\n",
		"missing target":  "rdr pass on lo0 inet proto tcp from any to any port 443 ->\n",
		"bad port":        "rdr pass on lo0 inet proto tcp from any to any port notaport -> 127.0.0.1 port 8080\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := pfValidateRules(rules); err == nil {
				t.Fatalf("validator accepted invalid rules: %q", rules)
			}
		})
	}
}

// TestPFRulesScopeToGroup: redirection must be scoped to the sandbox GID.
// An unscoped rule would capture the user's own traffic — their browser,
// their mail client — which is both a privacy problem and a support
// nightmare.
func TestPFRulesScopeToGroup(t *testing.T) {
	rules, err := pfRules(9999, 5000)
	if err != nil {
		t.Fatalf("pfRules: %v", err)
	}
	if !strings.Contains(rules, "group 5000") {
		t.Fatalf("rules are not scoped to the sandbox group:\n%s", rules)
	}
	// EVERY filter rule must carry the group. One unscoped line would
	// apply the policy machine-wide.
	for _, l := range strings.Split(rules, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "rdr ") {
			continue
		}
		if !strings.Contains(l, "group 5000") {
			t.Fatalf("unscoped filter rule would apply machine-wide: %q", l)
		}
	}
	// The pass rule is the one that diverts traffic; it MUST carry the
	// group. Check the specific line rather than the blob.
	var passLine string
	for _, l := range strings.Split(rules, "\n") {
		if strings.HasPrefix(l, "pass out") {
			passLine = l
		}
	}
	if passLine == "" {
		t.Fatal("no `pass out` rule rendered")
	}
	if !strings.Contains(passLine, "group 5000") {
		t.Fatalf("the diverting rule is unscoped: %q", passLine)
	}
}

// TestPFRulesRedirectOnly443 documents and enforces the port scope.
// Redirecting all ports would catch protocols whose destination we cannot
// recover from an SNI, silently breaking them.
func TestPFRulesRedirectOnly443(t *testing.T) {
	rules, err := pfRules(9999, 5000)
	if err != nil {
		t.Fatalf("pfRules: %v", err)
	}
	if strings.Contains(rules, "port = any") || strings.Contains(rules, "to any port any") {
		t.Fatalf("rules redirect more than 443:\n%s", rules)
	}
	if strings.Count(rules, "port 443") < 2 {
		t.Fatalf("expected the rdr and pass rules to scope to 443:\n%s", rules)
	}
}

// TestPFRulesUseRouteTo guards the subtle part. A plain `rdr` without
// `route-to lo0` loads cleanly, appears in pfctl output, and never fires for
// locally-originated traffic. That silent no-op is exactly the failure mode
// this project keeps hitting, so it is asserted rather than trusted.
func TestPFRulesUseRouteTo(t *testing.T) {
	rules, err := pfRules(9999, 5000)
	if err != nil {
		t.Fatalf("pfRules: %v", err)
	}
	if !strings.Contains(rules, "route-to lo0") {
		t.Fatalf("missing `route-to lo0`; the rdr will never fire for local traffic:\n%s", rules)
	}
}

func TestPFCleanOutputStripsBoilerplate(t *testing.T) {
	raw := []byte("pfctl: Use of -f option, could result in flushing of rules\n" +
		"present in the main ruleset added by the system at startup.\n" +
		"See /etc/pf.conf for further details.\n" +
		"stdin:1: syntax error\n")
	got := pfCleanOutput(raw)
	if got != "stdin:1: syntax error" {
		t.Fatalf("pfCleanOutput = %q, want the real error only", got)
	}
}

// TestPFRulesRejectBadGID: pf reserves INT_MAX as its "unknown gid"
// sentinel and rejects it at parse time. GID 0 is refused by us because
// scoping the redirect to wheel would capture root traffic machine-wide.
// Both are caught before any rule is emitted, discovered by probing the
// real parser rather than trusting documentation.
func TestPFRulesRejectBadGID(t *testing.T) {
	for name, gid := range map[string]int{
		"root/wheel":  0,
		"negative":    -1,
		"pf sentinel": 2147483647,
		"above max":   4294967295,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pfRules(8080, gid); err == nil {
				t.Fatalf("pfRules accepted an unusable GID %d", gid)
			}
		})
	}
}

// TestPFRulesRejectZeroPort: an unset port would render a rule redirecting
// to port 0, which loads but blackholes traffic.
func TestPFRulesRejectZeroPort(t *testing.T) {
	if _, err := pfRules(0, 5000); err == nil {
		t.Fatal("pfRules accepted a zero forwarder port")
	}
}

// TestPFRulesAreFailClosed is the most important test in this file.
//
// Transparent redirect forces the Seatbelt profile to ALLOW outbound 443,
// because Seatbelt adjudicates at connect() before a packet exists and pf
// can only redirect packets that exist (measured: a denied connect returns
// EPERM in 16ms while an allowed one takes the full 6s network timeout).
// That removes the OS-level deny which used to be the fail-closed
// guarantee, so the anchor itself must deny by default.
//
// Without a default-deny, a missing or flushed anchor means direct,
// silent, ungoverned egress. This test is what stops that shipping.
func TestPFRulesAreFailClosed(t *testing.T) {
	rules, err := pfRules(9999, 5000)
	if err != nil {
		t.Fatalf("pfRules: %v", err)
	}
	if !strings.Contains(rules, "block drop out inet from any to any group 5000") {
		t.Fatalf("no IPv4 default-deny; a flushed anchor would be fail-OPEN:\n%s", rules)
	}
	if !strings.Contains(rules, "block drop out inet6 from any to any group 5000") {
		t.Fatalf("no IPv6 default-deny; v6 egress would bypass governance:\n%s", rules)
	}

	// pf is LAST-match for filter rules: the deny must precede the allows.
	lines := strings.Split(strings.TrimSpace(rules), "\n")
	denyIdx, passIdx := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "block drop out inet ") && denyIdx == -1 {
			denyIdx = i
		}
		if strings.Contains(l, "route-to lo0") {
			passIdx = i
		}
	}
	if denyIdx == -1 || passIdx == -1 {
		t.Fatalf("expected both a default-deny and a redirect rule:\n%s", rules)
	}
	if denyIdx > passIdx {
		t.Fatalf("default-deny at %d comes AFTER the allow at %d; last-match "+
			"semantics would drop all traffic:\n%s", denyIdx, passIdx, rules)
	}
}

// TestPFRulesPermitDNS: name resolution happens in the sandboxed process,
// and the SNI we route by does not exist without it.
func TestPFRulesPermitDNS(t *testing.T) {
	rules, err := pfRules(9999, 5000)
	if err != nil {
		t.Fatalf("pfRules: %v", err)
	}
	if !strings.Contains(rules, "port 53") {
		t.Fatalf("DNS is blocked; the sandbox cannot resolve names:\n%s", rules)
	}
}

// TestPFRulesDenyArbitraryPorts: only 443 and DNS are permitted outbound.
// A rule permitting, say, 22 or 8080 would be an ungoverned egress channel.
func TestPFRulesDenyArbitraryPorts(t *testing.T) {
	rules, err := pfRules(9999, 5000)
	if err != nil {
		t.Fatalf("pfRules: %v", err)
	}
	for _, l := range strings.Split(rules, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "pass out") {
			continue
		}
		// Permitted: loopback (any port), 443, DNS.
		if strings.Contains(l, "on lo0") ||
			strings.Contains(l, "port 443") ||
			strings.Contains(l, "port 53") {
			continue
		}
		t.Fatalf("rule permits egress beyond 443/DNS/loopback: %q", l)
	}
}
