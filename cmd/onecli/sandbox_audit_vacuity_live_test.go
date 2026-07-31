//go:build darwin

package main

// Meta-test: is each mustBlock probe capable of DETECTING a hole?
//
// A probe that fails for an environmental reason — no IPv6 route, TCC
// consent never granted, a missing binary — reports a confident "ok"
// under the real profile while testing nothing at all. That is worse than
// having no probe, because it manufactures false assurance in exactly the
// place assurance matters.
//
// So every mustBlock probe is run against a profile with NO rules, where
// it must SUCCEED. If it fails even there, the probe cannot distinguish a
// working rule from a broken one and is rejected.
//
// This caught two real cases when it was written: `ipv6-literal` (this
// network has no IPv6 route) and `appleevent-fetch` (macOS TCC denies
// Apple Events independently of our sandbox). Both were reporting "ok"
// while proving nothing. They now carry preconditions and skip honestly.

import (
	"os"
	"os/exec"
	"testing"
)

func TestProbesCanDetectHoles(t *testing.T) {
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}

	// A profile that denies nothing: every escape must work here.
	const permissive = "(version 1)\n(allow default)\n"
	path := t.TempDir() + "/permissive.sb"
	if err := os.WriteFile(path, []byte(permissive), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, p := range sandboxProbes() {
		if p.want != mustBlock {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			if p.needs != "" {
				if _, err := os.Stat(p.needs); err != nil {
					t.Skipf("%s not present", p.needs)
				}
			}
			if p.precondition != nil {
				if ok, reason := p.precondition(); !ok {
					t.Skip(reason)
				}
			}
			argv := append([]string{"-f", path}, p.argv...)
			if out, err := exec.Command("/usr/bin/sandbox-exec", argv...).CombinedOutput(); err != nil {
				t.Fatalf("probe fails even with NO sandbox rules, so it cannot "+
					"detect a hole and its \"ok\" under the real profile is "+
					"meaningless.\nEither fix the probe or give it a precondition "+
					"that skips honestly.\nerr=%v out=%s", err, out)
			}
		})
	}
}
