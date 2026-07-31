//go:build darwin

package main

// THE FULL CHAIN, LIVE.
//
// Everything before this proved pieces: the listener tunnels by SNI (unit +
// live TLS), the pf rules parse and fire (measured: port 80 blackholed at
// 6004ms, port 443 refused at 2ms by the redirect). This joins them.
//
// What it proves: a process that dials a real host DIRECTLY, with no proxy
// configuration of any kind, is captured by pf, routed through our listener,
// and reaches its destination with credentials injected — while a process
// outside the sandbox group is untouched.
//
// That is the mechanism Cursor's extension host needs, end to end.
//
// Requires: setup complete (group, setgid helper, scoped pfctl sudo) and
// ONECLI_LIVE_CHAIN=1.

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func liveChainEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("ONECLI_LIVE_CHAIN") != "1" {
		t.Skip("set ONECLI_LIVE_CHAIN=1 to run the full-chain test")
	}
	if _, err := os.Stat(setgidHelperPath); err != nil {
		t.Skipf("setgid helper not installed: %v", err)
	}
	if err := verifyTransparentSetup(); err != nil {
		t.Skipf("transparent setup incomplete: %v", err)
	}
}

// recordingGateway is a real CONNECT proxy that records what it was asked
// for, so the test can assert the destination survived the trip.
type recordingGateway struct {
	ln   net.Listener
	mu   sync.Mutex
	seen []string
	auth []string
}

func newRecordingGateway(t *testing.T) *recordingGateway {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("gateway listen: %v", err)
	}
	g := &recordingGateway{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go g.handle(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return g
}

func (g *recordingGateway) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	head, err := readHeaderBlock(conn)
	if err != nil {
		return
	}
	lines := strings.Split(head, "\r\n")
	parts := strings.Fields(lines[0])
	if len(parts) < 2 || parts[0] != "CONNECT" {
		return
	}
	g.mu.Lock()
	g.seen = append(g.seen, parts[1])
	for _, l := range lines {
		if strings.HasPrefix(strings.ToLower(l), "proxy-authorization:") {
			g.auth = append(g.auth, strings.TrimSpace(l[len("proxy-authorization:"):]))
		}
	}
	g.mu.Unlock()

	up, err := net.DialTimeout("tcp", parts[1], 10*time.Second)
	if err != nil {
		_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer func() { _ = up.Close() }()
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	done := make(chan struct{})
	go func() { _, _ = copyConn(up, conn); close(done) }()
	_, _ = copyConn(conn, up)
	<-done
}

func (g *recordingGateway) snapshot() ([]string, []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.seen...), append([]string(nil), g.auth...)
}

func readHeaderBlock(c net.Conn) (string, error) {
	var head []byte
	one := make([]byte, 1)
	for {
		n, err := c.Read(one)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		head = append(head, one[0])
		if len(head) >= 4 && string(head[len(head)-4:]) == "\r\n\r\n" {
			return string(head), nil
		}
		if len(head) > 8192 {
			return "", fmt.Errorf("header too large")
		}
	}
}

func copyConn(dst, src net.Conn) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			w, werr := dst.Write(buf[:n])
			total += int64(w)
			if werr != nil {
				return total, werr
			}
		}
		if err != nil {
			if tc, ok := dst.(*net.TCPConn); ok {
				_ = tc.CloseWrite()
			}
			return total, err
		}
	}
}

// TestLiveFullChain is the demo, as an automated test.
func TestLiveFullChain(t *testing.T) {
	liveChainEnabled(t)

	gid, err := sandboxGID(transparentGroupName)
	if err != nil {
		t.Fatalf("resolving sandbox group: %v", err)
	}

	// A real CONNECT proxy standing in for the OneCLI gateway, so the test
	// asserts on what the gateway actually receives.
	gw := newRecordingGateway(t)
	creds := base64.StdEncoding.EncodeToString([]byte("x:aoc_live_chain"))

	// The transparent listener on a fixed port so the pf rule can name it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	go serveTransparent(ln, gw.ln.Addr().String(), creds)

	// Load the anchor pointing at this listener.
	rules, err := pfRules(port, gid)
	if err != nil {
		t.Fatalf("pfRules: %v", err)
	}
	if err := pfLoadAnchor(rules); err != nil {
		t.Fatalf("loading anchor (is `sudo pfctl` authorized?): %v", err)
	}
	t.Cleanup(func() { _ = pfFlushAnchor() })

	// The subject: curl, run under the sandbox group, with NO proxy
	// environment at all. It believes it is dialing example.com directly.
	cmd := exec.Command(setgidHelperPath, "curl", "-sS",
		"-o", "/dev/null", "-w", "%{http_code}", "--max-time", "20",
		"https://example.com/")
	cmd.Env = []string{"PATH=/usr/bin:/bin"} // deliberately no *_PROXY vars
	out, runErr := cmd.CombinedOutput()
	got := strings.TrimSpace(string(out))

	seen, auth := gw.snapshot()
	t.Logf("curl output: %q (err=%v)", got, runErr)
	t.Logf("gateway saw CONNECT for: %v", seen)

	if len(seen) == 0 {
		t.Fatalf("the gateway saw NO CONNECT: traffic did not traverse the "+
			"transparent listener (curl said %q, err %v)", got, runErr)
	}
	if seen[0] != "example.com:443" {
		t.Fatalf("gateway got CONNECT %q, want example.com:443", seen[0])
	}
	if len(auth) == 0 || auth[0] != "Basic "+creds {
		t.Fatalf("credentials were not injected: %v", auth)
	}
	if got != "200" {
		t.Fatalf("curl got %q, want 200: the tunnel did not carry the "+
			"request to completion", got)
	}
	t.Log("FULL CHAIN OK: direct dial -> pf redirect -> SNI recovery -> " +
		"credentialed CONNECT -> gateway -> 200, with no proxy config")
}

// TestLiveFullChainLeavesOthersAlone proves the blast radius is the group.
func TestLiveFullChainLeavesOthersAlone(t *testing.T) {
	liveChainEnabled(t)

	gid, err := sandboxGID(transparentGroupName)
	if err != nil {
		t.Fatalf("resolving sandbox group: %v", err)
	}
	gw := newRecordingGateway(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	go serveTransparent(ln, gw.ln.Addr().String(), "dGVzdA==")

	rules, err := pfRules(port, gid)
	if err != nil {
		t.Fatalf("pfRules: %v", err)
	}
	if err := pfLoadAnchor(rules); err != nil {
		t.Fatalf("loading anchor: %v", err)
	}
	t.Cleanup(func() { _ = pfFlushAnchor() })

	// Same request, NOT under the sandbox group: must be untouched.
	cmd := exec.Command("curl", "-sS", "-o", "/dev/null",
		"-w", "%{http_code}", "--max-time", "20", "https://example.com/")
	out, _ := cmd.CombinedOutput()
	if got := strings.TrimSpace(string(out)); got != "200" {
		t.Fatalf("an ungrouped process got %q; the anchor is affecting "+
			"traffic beyond the sandbox group", got)
	}
	if seen, _ := gw.snapshot(); len(seen) != 0 {
		t.Fatalf("ungrouped traffic was captured by the redirect: %v", seen)
	}
	t.Log("blast radius confirmed: only the sandbox group is redirected")
}
