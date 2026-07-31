//go:build darwin

package sandbox

// Tests for the transparent-redirect profile variant.
//
// The security-critical property is that the DEFAULT posture is unchanged:
// transparent mode permits outbound 443 (so pf has a packet to redirect),
// and that permission must never leak into an ordinary enforced run, where
// no anchor exists to govern it.

import (
	"strings"
	"testing"
)

// TestDefaultProfileDeniesDirect443 is the regression guard. If this ever
// fails, ordinary enforce runs have gained direct egress.
func TestDefaultProfileDeniesDirect443(t *testing.T) {
	p := profileForOpts("/Users/probe", Options{ForwarderPort: 4242})
	if strings.Contains(p, `"*:443"`) {
		t.Fatalf("the DEFAULT profile permits direct 443; enforce is no longer fail-closed:\n%s", p)
	}
	if !strings.Contains(p, "(deny network-outbound)") {
		t.Fatal("the default profile lost its network deny")
	}
	if !strings.Contains(p, `(allow network-outbound (remote tcp "localhost:4242"))`) {
		t.Fatal("the default profile lost its forwarder allow")
	}
}

// TestTransparentProfilePermits443 documents the deliberate widening.
func TestTransparentProfilePermits443(t *testing.T) {
	p := profileForOpts("/Users/probe", Options{ForwarderPort: 4242, Transparent: true})
	if !strings.Contains(p, `(allow network-outbound (remote tcp "*:443"))`) {
		t.Fatalf("transparent profile does not permit 443, so pf can never "+
			"see a packet to redirect:\n%s", p)
	}
	// The forwarder allow must survive: the redirect lands on loopback.
	if !strings.Contains(p, `(allow network-outbound (remote tcp "localhost:4242"))`) {
		t.Fatal("transparent profile lost its loopback allow")
	}
}

// TestTransparentProfileWidensOnly443 bounds the widening. Permitting any
// other port would create egress pf is not configured to capture, and the
// pf anchor's default-deny is the only thing that would catch it.
func TestTransparentProfileWidensOnly443(t *testing.T) {
	base := profileForOpts("/Users/probe", Options{ForwarderPort: 4242})
	tp := profileForOpts("/Users/probe", Options{ForwarderPort: 4242, Transparent: true})

	baseLines := map[string]bool{}
	for _, l := range strings.Split(base, "\n") {
		baseLines[strings.TrimSpace(l)] = true
	}
	var added []string
	for _, l := range strings.Split(tp, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, ";") || baseLines[t] {
			continue
		}
		added = append(added, t)
	}
	if len(added) != 1 {
		t.Fatalf("transparent mode changed %d rules, want exactly 1: %v", len(added), added)
	}
	if added[0] != `(allow network-outbound (remote tcp "*:443"))` {
		t.Fatalf("unexpected rule added by transparent mode: %q", added[0])
	}
}

// TestTransparentProfileKeepsEscapeDenials: the deferred-execution and
// credential-read denials are what stop a sandboxed process handing its work
// to an unsandboxed one. Transparent mode must not relax any of them.
func TestTransparentProfileKeepsEscapeDenials(t *testing.T) {
	p := profileForOpts("/Users/probe", Options{ForwarderPort: 4242, Transparent: true})
	for _, required := range []string{
		`(deny lsopen)`,
		`(deny appleevent-send)`,
		`(deny network-outbound (regex #"docker\.sock$"))`,
		`(deny file-write* (subpath "/Users/probe/Library/LaunchAgents"))`,
		`(deny file-write* (regex #"/\.git/hooks/"))`,
		`(deny file-read* (subpath "/Users/probe/.ssh"))`,
		`(deny file-write* (literal "/Users/probe/.onecli/enforce-wrap.sb"))`,
	} {
		if !strings.Contains(p, required) {
			t.Fatalf("transparent profile dropped a required denial: %s", required)
		}
	}
}

// TestProfilesRenderNoPlaceholders: an unexpanded {{NETWORK}} or {{HOME}}
// would load cleanly and match nothing — the silent-no-op failure this
// package exists to prevent.
func TestProfilesRenderNoPlaceholders(t *testing.T) {
	for name, p := range map[string]string{
		"default":     profileForOpts("/Users/probe", Options{ForwarderPort: 4242}),
		"transparent": profileForOpts("/Users/probe", Options{ForwarderPort: 4242, Transparent: true}),
	} {
		if strings.Contains(p, "{{") {
			t.Fatalf("%s profile has an unexpanded placeholder:\n%s", name, p)
		}
	}
}
