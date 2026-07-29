package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── parseGatewayProxy ───────────────────────────────────────────────────

func TestParseGatewayProxy(t *testing.T) {
	host, auth, err := parseGatewayProxy("http://x:aoc_secret@gw.example.com:10255")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "gw.example.com:10255" {
		t.Errorf("host = %q", host)
	}
	decoded, _ := base64.StdEncoding.DecodeString(auth)
	if string(decoded) != "x:aoc_secret" {
		t.Errorf("decoded auth = %q", decoded)
	}
}

func TestParseGatewayProxyRejectsCredentialless(t *testing.T) {
	if _, _, err := parseGatewayProxy("http://gw.example.com:10255"); err == nil {
		t.Fatal("expected error for URL without credentials")
	}
	if _, _, err := parseGatewayProxy(""); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

// ── parseEnforceForwarderArgs ───────────────────────────────────────────

func TestParseEnforceForwarderArgs(t *testing.T) {
	pid, ok := parseEnforceForwarderArgs([]string{enforceForwarderFlag, "--parent-pid", "1234"})
	if !ok || pid != 1234 {
		t.Errorf("got pid=%d ok=%v", pid, ok)
	}
	if _, ok := parseEnforceForwarderArgs([]string{"run", "--", "claude"}); ok {
		t.Error("normal argv must not parse as forwarder")
	}
	if _, ok := parseEnforceForwarderArgs([]string{enforceForwarderFlag}); ok {
		t.Error("missing --parent-pid must not parse")
	}
	if _, ok := parseEnforceForwarderArgs(nil); ok {
		t.Error("empty argv must not parse")
	}
}

// ── forwardConn ─────────────────────────────────────────────────────────

// startTestForwarder runs the accept loop against a local listener and
// returns its address.
func startTestForwarder(t *testing.T, upstreamAddr, basicAuth string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go forwardConn(conn, upstreamAddr, basicAuth)
		}
	}()
	return ln.Addr().String()
}

func TestForwardConnInjectsProxyAuthOnPlainHTTP(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Proxy-Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	auth := base64.StdEncoding.EncodeToString([]byte("x:aoc_tok"))
	fwdAddr := startTestForwarder(t, upstream.Listener.Addr().String(), auth)

	conn, err := net.Dial("tcp", fwdAddr)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer func() { _ = conn.Close() }()
	// An absolute-URI proxy-style GET.
	fmt.Fprintf(conn, "GET http://example.com/hello HTTP/1.1\r\nHost: example.com\r\n\r\n")
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(resp), "200 OK") {
		t.Errorf("response = %q", resp)
	}
	if gotAuth != "Basic "+auth {
		t.Errorf("upstream saw Proxy-Authorization = %q", gotAuth)
	}
}

func TestForwardConnRewritesConnectHeaders(t *testing.T) {
	// A fake gateway that records the CONNECT preamble then echoes.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	preamble := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		preamble <- string(buf[:n])
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
		// Echo one tunneled payload chunk back.
		n, _ = conn.Read(buf)
		_, _ = conn.Write(buf[:n])
	}()

	auth := base64.StdEncoding.EncodeToString([]byte("x:aoc_tok"))
	fwdAddr := startTestForwarder(t, ln.Addr().String(), auth)

	conn, err := net.Dial("tcp", fwdAddr)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer func() { _ = conn.Close() }()
	fmt.Fprintf(conn, "CONNECT db.example.com:443 HTTP/1.1\r\nHost: db.example.com:443\r\n\r\n")

	got := <-preamble
	if !strings.Contains(got, "CONNECT db.example.com:443 HTTP/1.1") {
		t.Errorf("gateway preamble = %q", got)
	}
	if !strings.Contains(got, "Proxy-Authorization: Basic "+auth) {
		t.Errorf("gateway preamble missing injected auth: %q", got)
	}

	// The 200 must reach the client, then the tunnel is raw bytes.
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read established: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "200 Connection Established") {
		t.Errorf("client saw %q", buf[:n])
	}
	payload := "tunneled-bytes"
	if _, err := io.WriteString(conn, payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf[:n]) != payload {
		t.Errorf("echo = %q, want %q", buf[:n], payload)
	}
}

func TestForwardConnUnreachableUpstreamReturns502(t *testing.T) {
	auth := base64.StdEncoding.EncodeToString([]byte("x:aoc_tok"))
	// A port that is closed: bind then immediately release it.
	tmp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadAddr := tmp.Addr().String()
	_ = tmp.Close()

	fwdAddr := startTestForwarder(t, deadAddr, auth)
	conn, err := net.Dial("tcp", fwdAddr)
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer func() { _ = conn.Close() }()
	fmt.Fprintf(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	resp, _ := io.ReadAll(conn)
	if !strings.Contains(string(resp), "502") {
		t.Errorf("response = %q, want 502", resp)
	}
}

// ── writeEnforceSettings ────────────────────────────────────────────────

func TestWriteEnforceSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := writeEnforceSettings(61234)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var got claudeSandboxSettings
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	if !got.Sandbox.Enabled || !got.Sandbox.FailIfUnavailable {
		t.Errorf("sandbox must be enabled + fail-closed: %+v", got.Sandbox)
	}
	if got.Sandbox.AllowUnsandboxedCommands {
		t.Error("the dangerouslyDisableSandbox escape hatch must be off")
	}
	if got.Sandbox.Network.HTTPProxyPort != 61234 {
		t.Errorf("proxy port = %d", got.Sandbox.Network.HTTPProxyPort)
	}
	if len(got.Sandbox.Network.AllowedDomains) != 1 || got.Sandbox.Network.AllowedDomains[0] != "*" {
		t.Errorf("allowedDomains = %v (host policy belongs to the gateway)", got.Sandbox.Network.AllowedDomains)
	}

	// Serialized keys must match Claude Code's schema exactly.
	for _, key := range []string{`"sandbox"`, `"enabled"`, `"failIfUnavailable"`,
		`"allowUnsandboxedCommands"`, `"httpProxyPort"`, `"allowedDomains"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("settings JSON missing key %s", key)
		}
	}

	if filepath.Dir(path) != filepath.Join(os.Getenv("HOME"), ".onecli") {
		t.Errorf("settings written outside ~/.onecli: %s", path)
	}
}

func TestEnforceAgentArgs(t *testing.T) {
	args := enforceAgentArgs("/home/u/.onecli/enforce-sandbox-settings.json")
	if len(args) != 2 || args[0] != "--settings" {
		t.Errorf("args = %v", args)
	}
}

// ── spawnEnforceForwarder validation ────────────────────────────────────

func TestSpawnEnforceForwarderRejectsBadProxyURL(t *testing.T) {
	if _, err := spawnEnforceForwarder(""); err == nil {
		t.Error("empty proxy URL must fail")
	}
	if _, err := spawnEnforceForwarder("http://no-creds.example.com:1"); err == nil {
		t.Error("credentialless proxy URL must fail")
	}
}
