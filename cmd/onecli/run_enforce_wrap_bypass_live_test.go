//go:build darwin

package main

// Live bypass suite for the enforce-wrap sandbox.
//
// The base live test (run_enforce_wrap_live_test.go) proves the happy
// path: loopback works, a direct dial doesn't. This file attacks the
// guarantee instead, by running the same escape matrix that
// `onecli sandbox audit` exposes to users (sandboxProbes in
// sandbox_audit.go). One table, two consumers: a vector added for users
// is automatically enforced in CI, so the shipped audit and the test
// suite can never drift apart.
//
// Every mustBlock vector here is an escape that env-var-only
// "enforcement" would miss — none of them read HTTPS_PROXY.

import (
	"os"
	"strings"
	"testing"

	"github.com/onecli/onecli-cli/internal/sandbox"
)

func TestLiveEnforceWrapBypasses(t *testing.T) {
	if err := sandbox.Available(); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
	// Port 1: nothing listens there, so every probe destination is outside
	// the allow. The forwarder-reachable probe is skipped by sandboxProbes()
	// when no real port is supplied.
	profile, err := sandbox.Materialize(1)
	if err != nil {
		t.Fatalf("materializing profile: %v", err)
	}

	for _, p := range sandboxProbes() {
		t.Run(p.name, func(t *testing.T) {
			res := runSandboxProbe(profile, p)
			if res.skipped {
				t.Skip(res.detail)
			}
			if !res.held {
				if p.want == mustBlock {
					t.Fatalf("SANDBOX HOLE: %s\nwhy it matters: %s", res.detail, p.why)
				}
				t.Fatalf("profile breaks a required capability: %s\nwhy it matters: %s", res.detail, p.why)
			}
		})
	}
}

// TestLiveSandboxAuditDetectsAHole is the audit's own regression test:
// it re-introduces the exact bug we shipped — denying the docker socket
// by its well-known path instead of its resolved one — and asserts the
// audit reports a hole.
//
// Without this, the audit could silently degrade into a test that always
// passes (a mis-typed rule name, a probe whose binary moved), and a
// green "no bypasses found" would be indistinguishable from a broken
// harness. A safety check nobody has ever seen fail is not evidence.
func TestLiveSandboxAuditDetectsAHole(t *testing.T) {
	if err := sandbox.Available(); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
	if _, err := os.Stat("/var/run/docker.sock"); err != nil {
		t.Skip("docker socket not present — this host cannot exercise the vector")
	}

	// The pre-fix profile: path-literal denies that never match, because
	// Seatbelt resolves the symlink first.
	base := sandbox.Profile(1)
	if base == "" {
		t.Skip("no profile available on this host")
	}
	holed := strings.Replace(base,
		`(deny network-outbound (regex #"docker\.sock$"))`,
		`(deny network-outbound (remote unix-socket (path-literal "/var/run/docker.sock")))`,
		1)
	if holed == base {
		t.Fatal("profile no longer contains the docker regex deny — update this test to match the current rule")
	}

	path := t.TempDir() + "/holed.sb"
	if err := os.WriteFile(path, []byte(holed), 0o600); err != nil {
		t.Fatal(err)
	}

	var docker sandboxProbe
	for _, p := range sandboxProbes() {
		if p.name == "docker-socket" {
			docker = p
		}
	}
	if docker.name == "" {
		t.Fatal("docker-socket probe missing from the escape matrix")
	}

	if res := runSandboxProbe(path, docker); res.held {
		t.Fatalf("audit did NOT detect a known hole — the harness is not actually testing anything: %s", res.detail)
	}
}
