//go:build darwin

package sandbox

// Live test for the enforce-wrap Seatbelt profile: verifies on a real
// macOS host that the OS (1) blocks direct egress from inside the
// sandbox and (2) allows the loopback path — the two properties wrap
// mode's guarantee rests on. Local-only: the "allowed" leg talks to a
// listener the test owns, so no external network is required.

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLiveProfileConfinesEgress(t *testing.T) {
	if err := available(); err != nil {
		t.Skipf("sandbox unavailable: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	// The listener comes FIRST: the profile's network allow is scoped to
	// the forwarder's port, so the port has to exist before the profile
	// can be rendered.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "wrap-ok")
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	profile, err := materialize(uint16(ln.Addr().(*net.TCPAddr).Port))
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	curl := func(args ...string) (string, error) {
		argv := append([]string{"-f", profile, "/usr/bin/curl", "-sS", "--max-time", "5"}, args...)
		out, err := exec.Command(sandboxExecPath, argv...).CombinedOutput()
		return string(out), err
	}

	t.Run("loopback allowed", func(t *testing.T) {
		out, err := curl(fmt.Sprintf("http://%s/", ln.Addr()))
		if err != nil || !strings.Contains(out, "wrap-ok") {
			t.Errorf("loopback request failed: %v %q", err, out)
		}
	})

	t.Run("direct egress denied at the OS", func(t *testing.T) {
		// Raw IP, so no DNS involved: connect() itself must be refused
		// by Seatbelt (instantly), not time out on a missing network.
		start := time.Now()
		out, err := curl("http://1.1.1.1/")
		if err == nil {
			t.Fatalf("direct egress unexpectedly succeeded: %q", out)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("denial took %v — looks like a network timeout, not a sandbox denial", d)
		}
	})

	t.Run("children inherit the sandbox", func(t *testing.T) {
		// bash -> curl: the grandchild must still be confined.
		argv := []string{"-f", profile, "/bin/bash", "-c", "/usr/bin/curl -sS --max-time 5 http://1.1.1.1/"}
		out, err := exec.Command(sandboxExecPath, argv...).CombinedOutput()
		if err == nil {
			t.Errorf("grandchild egress unexpectedly succeeded: %q", out)
		}
	})
}
