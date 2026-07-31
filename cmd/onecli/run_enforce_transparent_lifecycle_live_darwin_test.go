//go:build darwin

package main

// Lifecycle tests for the transparent session: the anchor must not outlive
// the run.
//
// This is a regression guard for a real leak. `onecli run` ends in
// syscall.Exec, which replaces the process image, so the parent's deferred
// Close and signal handlers never execute. The anchor persisted
// indefinitely after a run ended (measured: rdr=1 still present at t+10s),
// leaving the sandbox group redirected to a dead port and breaking the next
// enforced run in a confusing way.
//
// Cleanup therefore belongs to the detached sidecar, the only component
// that outlives the exec and can observe the agent exiting.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func liveLifecycleEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("ONECLI_LIVE_CHAIN") != "1" {
		t.Skip("set ONECLI_LIVE_CHAIN=1 to run anchor lifecycle tests")
	}
	if _, err := os.Stat(setgidHelperPath); err != nil {
		t.Skipf("setgid helper not installed: %v", err)
	}
	if err := verifyTransparentSetup(); err != nil {
		t.Skipf("transparent setup incomplete: %v", err)
	}
}

// anchorRuleCount reports how many rules our anchor currently holds.
func anchorRuleCount(t *testing.T) int {
	t.Helper()
	loaded, err := pfAnchorLoaded()
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(loaded, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// TestAnchorDoesNotOutliveTheRun is the regression guard.
func TestAnchorDoesNotOutliveTheRun(t *testing.T) {
	liveLifecycleEnabled(t)

	// Start clean so the assertion is unambiguous.
	_ = pfFlushAnchor()
	if n := anchorRuleCount(t); n != 0 {
		t.Fatalf("anchor is not clean before the test: %d rules", n)
	}

	// A session models what a run installs.
	sess, err := startTransparentSession("http://x:aoc_test@127.0.0.1:10255/")
	if err != nil {
		t.Fatalf("starting session: %v", err)
	}
	if n := anchorRuleCount(t); n == 0 {
		_ = sess.Close()
		t.Fatal("the session loaded no rules; it is not governing anything")
	}

	// Close covers the early-failure path (before exec). The normal path is
	// the sidecar, exercised by TestSidecarFlushesAnchorOnParentExit.
	if err := sess.Close(); err != nil {
		t.Fatalf("closing session: %v", err)
	}
	if n := anchorRuleCount(t); n != 0 {
		t.Fatalf("anchor still holds %d rules after Close; the sandbox group "+
			"stays redirected to a dead port", n)
	}
}

// TestSidecarFlushesAnchorOnParentExit exercises the NORMAL teardown path
// end to end through the real binary: a run that starts, installs the
// anchor, and exits. Nothing may be left behind.
func TestSidecarFlushesAnchorOnParentExit(t *testing.T) {
	liveLifecycleEnabled(t)

	bin := os.Getenv("ONECLI_TEST_BINARY")
	if bin == "" {
		t.Skip("set ONECLI_TEST_BINARY to the built onecli binary")
	}
	_ = pfFlushAnchor()

	cmd := exec.Command(bin, "run", "--enforce", "--", "bash", "-c", "true")
	cmd.Env = append(os.Environ(), enforceTransparentEnv+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("enforced run did not start (env-dependent): %v\n%s", err, out)
	}

	// The sidecar polls its parent every enforceParentPollTime, so allow a
	// few intervals before declaring a leak.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if anchorRuleCount(t) == 0 {
			return // cleaned up
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("the anchor still holds %d rules after the run exited; "+
		"group %s stays redirected with no listener",
		anchorRuleCount(t), transparentGroupName)
}
