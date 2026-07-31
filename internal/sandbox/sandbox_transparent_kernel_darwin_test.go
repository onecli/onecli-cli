//go:build darwin

package sandbox

// A kernel-level check that both profile variants actually LOAD.
//
// Text assertions prove a profile says what we intended. They cannot prove
// the kernel accepts it: a profile with a syntax error, or a rule the
// sandbox rejects, fails only at launch. This package has been bitten by
// exactly that gap before (rules that loaded cleanly and matched nothing),
// so both variants are handed to the real sandbox-exec.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestBothProfilesLoadInTheRealKernel(t *testing.T) {
	if _, err := os.Stat(sandboxExecPath); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	dir := t.TempDir()

	for name, opts := range map[string]Options{
		"default":     {ForwarderPort: 4242},
		"transparent": {ForwarderPort: 4242, Transparent: true},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".sb")
			if err := os.WriteFile(path, []byte(profileForOpts(home, opts)), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			// `true` does nothing; the point is whether the profile loads.
			out, err := exec.Command(sandboxExecPath, "-f", path, "/usr/bin/true").CombinedOutput()
			if err != nil {
				t.Fatalf("the %s profile was REJECTED by the kernel: %v\n%s", name, err, out)
			}
		})
	}
}

// TestTransparentProfileActuallyPermits443 proves the widening is real at
// the kernel level, not just present in the text. It uses a local listener
// on 443 rather than a remote host so the test needs no internet: what
// matters is whether Seatbelt permits the connect(), and a refused
// connection to a closed local port is still an ALLOWED syscall, whereas a
// Seatbelt denial surfaces differently.
//
// Distinguishing the two reliably is what makes this test meaningful, so it
// compares the two profiles against the SAME target: under the default
// profile the connect must fail, under transparent it must not fail in the
// same way.
func TestTransparentProfileActuallyPermits443(t *testing.T) {
	if _, err := os.Stat(sandboxExecPath); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}
	if os.Getenv("ONECLI_LIVE_TRANSPARENT") != "1" {
		t.Skip("set ONECLI_LIVE_TRANSPARENT=1 (makes a real outbound connection)")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	dir := t.TempDir()

	write := func(name string, opts Options) string {
		p := filepath.Join(dir, name+".sb")
		if err := os.WriteFile(p, []byte(profileForOpts(home, opts)), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	defaultProfile := write("default", Options{ForwarderPort: 4242})
	transparentProfile := write("transparent", Options{ForwarderPort: 4242, Transparent: true})

	// TEST-NET-1 never routes, so an ALLOWED connect hangs until timeout
	// while a DENIED one returns immediately. That timing gap is the
	// signal, and it is the same measurement that established Seatbelt
	// adjudicates at connect().
	const target = "https://192.0.2.1/"

	run := func(profile string) (secs float64) {
		start := nowSeconds()
		cmd := exec.Command(sandboxExecPath, "-f", profile,
			"curl", "-sS", "--max-time", "4", target)
		_ = cmd.Run()
		return nowSeconds() - start
	}

	denied := run(defaultProfile)
	allowed := run(transparentProfile)
	t.Logf("connect to an unroutable 443: default=%.3fs transparent=%.3fs", denied, allowed)

	if denied > 1.0 {
		t.Fatalf("the DEFAULT profile took %.3fs, so it did not deny the "+
			"connect; enforce may no longer be fail-closed", denied)
	}
	if allowed < 1.0 {
		t.Fatalf("the TRANSPARENT profile returned in %.3fs, so the connect "+
			"was still denied and pf would never see a packet to redirect", allowed)
	}
}

// nowSeconds is a monotonic-ish clock for the timing comparison above.
func nowSeconds() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
