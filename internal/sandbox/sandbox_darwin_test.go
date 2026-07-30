//go:build darwin

package sandbox

// Unit tests for the Seatbelt profile. They pin SYNTAX; the behavioural
// proof that each rule actually fires lives in the live bypass suite and
// in `onecli sandbox audit`. Both matter: the docker.sock rule passed a
// text assertion for its entire life while matching nothing at runtime.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileShape(t *testing.T) {
	// The profile is the enforcement layer — pin its structural
	// invariants so a refactor can't silently weaken it. These assertions
	// pin SYNTAX only; behavioural proof lives in TestLiveEnforceWrapBypasses
	// and `onecli sandbox audit`. The distinction is not academic: the
	// docker.sock rule below passed a text assertion for its entire life
	// while matching nothing at runtime.
	profile := profileFor("/Users/probe", 4242)
	for _, want := range []string{
		"(deny network-outbound)",
		// Scoped to the forwarder's PORT, not "localhost:*". Seatbelt's
		// localhost means any address local to this machine, including LAN
		// interfaces, so the wildcard let a sandboxed agent reach any
		// host-bound listener (a Docker published port, a dev server) and
		// relay out through it. Demonstrated end-to-end before this change.
		`(allow network-outbound (remote tcp "localhost:4242"))`,
		"(allow network-outbound (remote unix-socket))",
		// A REGEX on the socket name, not a path-literal: Seatbelt matches
		// the RESOLVED path and Docker Desktop symlinks /var/run/docker.sock
		// into ~/.docker/run, so the literal form loaded fine and never
		// matched. Text assertions can only pin syntax — the behavioural
		// proof is TestLiveEnforceWrapBypasses/docker-socket.
		`(deny network-outbound (regex #"docker\.sock$"))`,
		"(deny lsopen)",
		"(deny appleevent-send)",
		// Deferred egress: surfaces where a sandboxed write is executed
		// LATER by an unsandboxed process. All were reachable until these
		// rules existed, so the profile was never network-only in effect.
		`(deny file-write* (subpath "/Users/probe/Library/LaunchAgents"))`,
		`(literal "/Users/probe/.zshrc")`,
		`(deny file-write* (subpath "/usr/local/bin"))`,
		// Scoped to the enforcement artifacts, NOT all of ~/.onecli: the
		// directory also holds config.json and credentials that onecli's
		// own commands write, and a broad deny bricked `onecli config set`
		// inside an enforced session.
		`(deny file-write* (literal "/Users/probe/.onecli/enforce-wrap.sb"))`,
		`(deny file-write* (literal "/Users/probe/.onecli/enforce-sandbox-settings.json"))`,
		`(deny file-read* (subpath "/Users/probe/.ssh"))`,
	} {
		if !strings.Contains(profile, want) {
			t.Errorf("profile missing %q", want)
		}
	}
	if !strings.HasPrefix(profile, "(version 1)") {
		t.Error("profile must start with (version 1)")
	}
	// The deny must come BEFORE the loopback allow: Seatbelt applies the
	// last matching rule, so an allow preceding the deny would be dead.
	deny := strings.Index(profile, "(deny network-outbound)")
	allow := strings.Index(profile, `(allow network-outbound (remote tcp "localhost:4242"))`)
	// Index returns -1 when absent, which would make the ordering check
	// pass vacuously against a renamed rule. Require both to exist first.
	if deny < 0 || allow < 0 {
		t.Fatalf("network rules not found (deny=%d allow=%d) — update this test", deny, allow)
	}
	if deny > allow {
		t.Error("(deny network-outbound) must precede the loopback allow")
	}
}

// The profile is a Go raw string literal, so a backtick anywhere in it —
// including inside a comment, e.g. quoting a shell command — silently
// terminates the literal and produces a confusing parse error far from
// the real cause. It cost three build breaks while writing these rules,
// so it gets a test with a message that names the fix.
// The wildcard-port form is the exact bug that allowed LAN relay. Assert
// it can never return: it reads as "loopback only" but Seatbelt treats it
// as "any local address", which is how it survived review the first time.
func TestProfileHasNoWildcardLocalAllow(t *testing.T) {
	if strings.Contains(seatbeltProfileTemplate, `(remote ip "localhost:*")`) {
		t.Error(`profile allows "localhost:*", which includes the host's LAN ` +
			`interfaces and lets an agent relay out through any host-bound ` +
			`listener — scope the allow to the forwarder port instead`)
	}
}

func TestProfileHasNoBackticks(t *testing.T) {
	if strings.Contains(seatbeltProfileTemplate, "`") {
		t.Error("profile contains a backtick, which terminates the raw string literal early — " +
			"use single quotes when quoting commands in profile comments")
	}
}

// Seatbelt applies the LAST matching rule, so an allow must follow the
// deny it carves out of. The .sample exception is load-bearing (without
// it `git clone` fails), and reordering would silently disable it while
// still loading cleanly.
func TestSampleCarveOutFollowsDeny(t *testing.T) {
	profile := profileFor("/Users/probe", 4242)
	deny := strings.Index(profile, `(deny file-write* (regex #"/\.git/hooks/"))`)
	allow := strings.Index(profile, `(allow file-write* (regex #"/\.git/hooks/[^/]*\.sample$"))`)
	if deny < 0 || allow < 0 {
		t.Fatalf("git hook rules not found (deny=%d allow=%d)", deny, allow)
	}
	if allow < deny {
		t.Error("the .sample allow must come AFTER the hooks deny, or git clone breaks")
	}
}

func TestMaterializeWritesRenderedProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, err := materialize(4242)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading profile: %v", err)
	}
	home, _ := os.UserHomeDir()
	if string(data) != profileFor(home, 4242) {
		t.Error("written profile differs from the rendered template")
	}
	// The template must be MATERIALIZED: Seatbelt does no $HOME expansion,
	// so a leaked placeholder would load cleanly and match nothing — the
	// same silent-no-op shape as the docker.sock bug.
	if strings.Contains(string(data), "{{HOME}}") {
		t.Error("profile still contains an unexpanded {{HOME}} placeholder")
	}
	if filepath.Base(filepath.Dir(path)) != ".onecli" {
		t.Errorf("profile should live under ~/.onecli, got %s", path)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("profile mode = %v, want 0600", info.Mode().Perm())
	}

	// Second call: idempotent, same path.
	again, err := materialize(4242)
	if err != nil || again != path {
		t.Errorf("second write: path=%s err=%v, want %s nil", again, err, path)
	}
}
