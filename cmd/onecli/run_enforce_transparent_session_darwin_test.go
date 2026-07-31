//go:build darwin

package main

// Tests for the transparent session lifecycle.
//
// The property under test is the one that keeps the design honest: the
// widened Seatbelt profile and the loaded pf anchor must exist together or
// not at all. These run without root by exercising the failure paths, which
// is where the security-relevant behavior lives.

import (
	"strings"
	"testing"
)

// TestTransparentSessionRefusesWithoutSetup is the guard that matters most.
// Transparent mode permits outbound 443 in the sandbox profile; if a session
// could start without a verified anchor, that permission would be
// ungoverned direct egress.
//
// On a machine without setup, startTransparentSession MUST fail.
func TestTransparentSessionRefusesWithoutSetup(t *testing.T) {
	if err := verifyTransparentSetup(); err == nil {
		t.Skip("this machine IS set up; the refusal path cannot be tested here")
	}
	_, err := startTransparentSession("http://x:aoc_test@127.0.0.1:1/")
	if err == nil {
		t.Fatal("a transparent session started without verified setup; the " +
			"widened profile would be ungoverned")
	}
	t.Logf("correctly refused: %v", err)
}

// TestVerifyTransparentSetupNamesTheFailure: a generic "not ready" would
// leave an operator guessing between four different causes, two of which
// (pf disabled, anchor unreferenced) fail silently at the pf layer.
func TestVerifyTransparentSetupNamesTheFailure(t *testing.T) {
	err := verifyTransparentSetup()
	if err == nil {
		t.Skip("this machine is fully set up")
	}
	msg := err.Error()
	known := []string{
		transparentGroupName, // group missing
		"pf is disabled",
		"does not reference",
		"pfctl",
	}
	for _, k := range known {
		if strings.Contains(msg, k) {
			t.Logf("failure names its cause: %v", err)
			return
		}
	}
	t.Fatalf("error does not identify which precondition failed: %v", err)
}

// TestTransparentSetupScriptIsSafe reviews the generated script for the
// properties that make it trustworthy. It edits sudoers, so its content is
// a security decision, not a formatting one.
func TestTransparentSetupScriptIsSafe(t *testing.T) {
	script := transparentSetupScript(700, "testuser")

	// Scoped privilege only: pfctl and nothing else.
	if !strings.Contains(script, "NOPASSWD: "+pfctlPath) {
		t.Fatal("script does not grant pfctl access")
	}
	for _, forbidden := range []string{
		"NOPASSWD: ALL",
		"ALL=(ALL)",
		"(ALL:ALL)",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("script grants blanket root via %q", forbidden)
		}
	}

	// Must validate its own sudoers edit: a malformed sudoers file can
	// lock the user out of sudo entirely.
	if !strings.Contains(script, "visudo -cf") {
		t.Fatal("script does not validate the sudoers file it writes")
	}

	// Idempotent: guarded group creation.
	if !strings.Contains(script, "if ! dscl") {
		t.Fatal("script is not idempotent for group creation")
	}

	// Must tell the user how to undo it.
	if !strings.Contains(script, "To undo") {
		t.Fatal("script does not print an undo path")
	}

	// Must not use a system GID.
	if strings.Contains(script, "PrimaryGroupID 0") {
		t.Fatal("script would create a group with GID 0")
	}
}

// TestTransparentSetupScriptRejectsSystemGID guards the GID floor.
func TestNextFreeGIDIsAboveSystemRange(t *testing.T) {
	gid, err := nextFreeGID()
	if err != nil {
		t.Skipf("cannot enumerate groups: %v", err)
	}
	if gid < transparentGIDFloor {
		t.Fatalf("nextFreeGID returned %d, below the system floor %d",
			gid, transparentGIDFloor)
	}
	if err := validateSandboxGID(gid); err != nil {
		t.Fatalf("nextFreeGID returned a GID pf cannot use: %v", err)
	}
}

// TestTransparentSessionCloseIsIdempotent: Close runs from a signal handler
// AND from the normal path, so a double call must not error or panic.
func TestTransparentSessionCloseIsIdempotent(t *testing.T) {
	s := &transparentSession{} // nothing loaded, nothing bound
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
