//go:build darwin

package main

// Live end-to-end proof for transparent redirect.
//
// Gated behind ONECLI_LIVE_TRANSPARENT=1 because it makes real network
// connections. What it proves cannot be proven by a unit test: that a real
// TLS client, given NO proxy configuration whatsoever, completes a real
// handshake with a real remote host purely because its connection landed on
// the transparent listener.
//
// This is the exact shape of the Cursor extension-host case. The listener
// stands in for what pf produces; pf's own contribution (getting the packet
// here) is verified separately by the rule tests and the anchor check.

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func liveTransparentEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("ONECLI_LIVE_TRANSPARENT") != "1" {
		t.Skip("set ONECLI_LIVE_TRANSPARENT=1 to run live transparent-redirect tests")
	}
}

// directCONNECTGateway is a minimal real proxy: it honors CONNECT by dialing
// the true destination. It stands in for the OneCLI gateway so this test
// exercises the listener against genuine remote endpoints without depending
// on gateway availability or credentials.
type directCONNECTGateway struct {
	ln       net.Listener
	sawHosts chan string
}

func newDirectGateway(t *testing.T) *directCONNECTGateway {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("gateway listen: %v", err)
	}
	g := &directCONNECTGateway{ln: ln, sawHosts: make(chan string, 16)}
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

func (g *directCONNECTGateway) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	var head []byte
	one := make([]byte, 1)
	for {
		n, err := conn.Read(one)
		if err != nil || n == 0 {
			return
		}
		head = append(head, one[0])
		if len(head) >= 4 && string(head[len(head)-4:]) == "\r\n\r\n" {
			break
		}
		if len(head) > 8192 {
			return
		}
	}
	line := strings.Split(string(head), "\r\n")[0]
	parts := strings.Fields(line)
	if len(parts) < 2 || parts[0] != "CONNECT" {
		return
	}
	target := parts[1]
	select {
	case g.sawHosts <- target:
	default:
	}

	upstream, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer func() { _ = upstream.Close() }()
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	go func() { _, _ = io.Copy(upstream, conn) }()
	_, _ = io.Copy(conn, upstream)
}

// TestLiveTransparentCompletesRealTLS is the proof.
//
// A client with no proxy config dials the transparent listener and completes
// a genuine TLS handshake with a real host, verifying the real certificate.
// A working handshake is unforgeable evidence the ClientHello was replayed
// intact and bytes flow correctly in both directions.
func TestLiveTransparentCompletesRealTLS(t *testing.T) {
	liveTransparentEnabled(t)

	gw := newDirectGateway(t)
	creds := base64.StdEncoding.EncodeToString([]byte("x:test"))
	addr := startTransparent(t, gw.ln.Addr().String(), creds)

	for _, host := range []string{
		"api.anthropic.com",
		"agentn.global.api5.cursor.sh", // the host that failed under enforce
	} {
		t.Run(host, func(t *testing.T) {
			raw, err := net.DialTimeout("tcp", addr, 10*time.Second)
			if err != nil {
				t.Fatalf("dial listener: %v", err)
			}
			defer func() { _ = raw.Close() }()

			// Full verification against the real chain. If the listener
			// mangled the handshake this fails.
			tlsConn := tls.Client(raw, &tls.Config{ServerName: host})
			_ = tlsConn.SetDeadline(time.Now().Add(20 * time.Second))
			if err := tlsConn.Handshake(); err != nil {
				t.Fatalf("TLS handshake through the transparent listener failed: %v", err)
			}
			cs := tlsConn.ConnectionState()
			if len(cs.PeerCertificates) == 0 {
				t.Fatal("no peer certificate")
			}
			if err := tlsConn.VerifyHostname(host); err != nil {
				t.Fatalf("certificate does not match %s: %v", host, err)
			}
			t.Logf("handshake ok: %s, tls=%#x, cn=%q",
				host, cs.Version, cs.PeerCertificates[0].Subject.CommonName)
		})
	}

	// The gateway must have seen a CONNECT for each host, proving the
	// destination was recovered from the SNI rather than guessed.
	close(gw.sawHosts)
	var seen []string
	for h := range gw.sawHosts {
		seen = append(seen, h)
	}
	if len(seen) < 2 {
		t.Fatalf("gateway saw %v; expected a CONNECT per host", seen)
	}
}

// TestLiveTransparentServesRealHTTP proves the tunnel carries application
// traffic, not just a handshake: a full HTTPS request/response round trip
// through a client that was never told a proxy exists.
func TestLiveTransparentServesRealHTTP(t *testing.T) {
	liveTransparentEnabled(t)

	gw := newDirectGateway(t)
	creds := base64.StdEncoding.EncodeToString([]byte("x:test"))
	addr := startTransparent(t, gw.ln.Addr().String(), creds)

	// A transport whose DialContext always lands on the listener: exactly
	// what pf does to a process that dials directly. Note there is no
	// Proxy field set — the client has no proxy configuration at all, and
	// still ends up governed.
	tr := &http.Transport{Proxy: nil}
	tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	c := &http.Client{Timeout: 30 * time.Second, Transport: tr}

	resp, err := c.Get("https://api.anthropic.com/v1/messages")
	if err != nil {
		t.Fatalf("HTTPS request through the transparent listener failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Any HTTP status proves the round trip; the endpoint rejects an
	// unauthenticated POST-shaped GET, which is fine.
	if resp.StatusCode == 0 {
		t.Fatal("no HTTP response")
	}
	t.Logf("round trip ok: HTTP %d from api.anthropic.com with NO proxy config",
		resp.StatusCode)
}
