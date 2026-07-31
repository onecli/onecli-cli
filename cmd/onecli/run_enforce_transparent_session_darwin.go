//go:build darwin

package main

// Session lifecycle for transparent redirect: verify preconditions, spawn
// the listener, load the anchor, and guarantee it is removed on exit.
//
// The invariant this file exists to hold: the widened Seatbelt profile and
// the loaded pf anchor must be installed together and removed together.
// Transparent mode permits outbound 443 at the OS layer, and only the
// anchor's default-deny governs it. A widened profile with no anchor is
// ungoverned direct egress, so every path here either achieves BOTH or
// fails and cleans up.

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// transparentSession owns the resources an enforced run installs.
type transparentSession struct {
	Port   uint16
	GID    int
	loaded bool
}

// startTransparentSession verifies setup, binds the listener, and loads the
// anchor. Returns a session whose Close() restores the machine.
//
// Fails closed at every step: any error tears down what was already done, so
// a partial failure never leaves a widened profile with no anchor.
func startTransparentSession(gatewayProxyURL string) (*transparentSession, error) {
	// pf is reference counted on macOS and can be off even after setup
	// enabled it, so bring it up before verifying rather than failing on a
	// condition we can fix.
	if err := pfEnsureEnabled(); err != nil {
		return nil, err
	}
	if err := verifyTransparentSetup(); err != nil {
		return nil, err
	}
	gid, err := sandboxGID(transparentGroupName)
	if err != nil {
		return nil, err
	}
	if _, _, err := parseGatewayProxy(gatewayProxyURL); err != nil {
		return nil, err
	}

	// A DETACHED sidecar, not an in-process goroutine: `onecli run` ends in
	// syscall.Exec, which replaces this process image with the agent. An
	// in-process listener would die there while the anchor survived, and
	// every redirected connection would hit a dead port. Observed exactly
	// that way before this changed (gid=700 adopted, curl RST in 2ms).
	port, err := spawnTransparentListener(gatewayProxyURL)
	if err != nil {
		return nil, err
	}

	s := &transparentSession{Port: port, GID: gid}

	rules, err := pfRules(port, gid)
	if err != nil {
		return nil, err
	}
	if err := pfLoadAnchor(rules); err != nil {
		return nil, err
	}
	s.loaded = true

	// Verify the anchor is REALLY loaded rather than trusting the exit
	// code. pfctl can succeed while rules end up inert, and this codebase
	// has been bitten by exactly that shape before.
	if err := s.verifyLoaded(); err != nil {
		_ = s.Close()
		return nil, err
	}

	// The anchor outlives a crashed process unless we remove it. Signal
	// handling is not politeness here: a stale anchor would keep
	// redirecting a group whose listener is gone, breaking all egress for
	// it until someone flushed it by hand.
	s.installCleanupHandlers()
	return s, nil
}

// verifyLoaded confirms our rules are present in the anchor.
func (s *transparentSession) verifyLoaded() error {
	loaded, err := pfAnchorLoaded()
	if err != nil {
		return err
	}
	if loaded == "" {
		return fmt.Errorf("the pf anchor is empty after loading; rules would not apply")
	}
	// The redirect target port is the one thing that must match exactly:
	// a stale anchor from a previous session would point at a dead port.
	want := fmt.Sprintf("port %d", s.Port)
	if !strings.Contains(loaded, want) {
		return fmt.Errorf("the loaded anchor does not target this session's "+
			"listener (%s); a stale anchor may be present", want)
	}
	return nil
}

// installCleanupHandlers removes the anchor on SIGINT/SIGTERM.
func (s *transparentSession) installCleanupHandlers() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-ch
		_ = s.Close()
		os.Exit(130)
	}()
}

// Close flushes the anchor, for the paths where this process still exists
// to do it: a failure during setup, or a run that returns before exec.
//
// It is NOT the normal teardown path. `onecli run` ends in syscall.Exec,
// which replaces this process image, so neither defers nor signal handlers
// installed here survive. The detached sidecar owns cleanup for the normal
// case, and this covers the early-failure cases it cannot see.
//
// Safe to call twice.
func (s *transparentSession) Close() error {
	var firstErr error
	if s.loaded {
		if err := pfFlushAnchor(); err != nil {
			firstErr = err
		}
		s.loaded = false
	}
	return firstErr
}
