package main

// Live smoke test for the enforce forwarder: spawns the real detached
// sidecar against the gateway named in ONECLI_SMOKE_GATEWAY_PROXY_URL and
// issues a CONNECT through it without any credentials, verifying the
// forwarder injects them. Skipped unless the env var is set.

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveForwarderSmoke(t *testing.T) {
	proxyURL := os.Getenv("ONECLI_SMOKE_GATEWAY_PROXY_URL")
	if proxyURL == "" {
		t.Skip("set ONECLI_SMOKE_GATEWAY_PROXY_URL to run the live smoke test")
	}

	port, err := spawnEnforceForwarder(proxyURL)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Logf("forwarder on 127.0.0.1:%d", port)
	time.Sleep(300 * time.Millisecond) // let the child pick up the listener

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial forwarder: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// No Proxy-Authorization here — the forwarder must add it.
	fmt.Fprintf(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read: %v", err)
	}
	resp := string(buf[:n])
	t.Logf("gateway said: %q", strings.SplitN(resp, "\r\n", 2)[0])
	if !strings.Contains(resp, "200") {
		t.Errorf("expected 200 Connection Established, got %q", resp)
	}
}
