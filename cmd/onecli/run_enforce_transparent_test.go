package main

// End-to-end tests for the transparent listener.
//
// The scenario reproduced here is exactly the Cursor extension-host failure:
// a client that speaks NO proxy protocol, dials what it believes is a remote
// host, and states its destination only through the TLS SNI. These tests do
// not require pf or root — the redirect is simulated by dialing the listener
// directly, which is precisely what pf produces.

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGateway accepts CONNECT, records the request line and credentials, and
// echoes tunnel bytes back so the client can verify the pipe.
type fakeGateway struct {
	ln       net.Listener
	mu       sync.Mutex
	requests []string
	authz    []string
	refuse   bool
}

func newFakeGateway(t *testing.T, refuse bool) *fakeGateway {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("gateway listen: %v", err)
	}
	g := &fakeGateway{ln: ln, refuse: refuse}
	go g.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return g
}

func (g *fakeGateway) addr() string { return g.ln.Addr().String() }

func (g *fakeGateway) serve() {
	for {
		conn, err := g.ln.Accept()
		if err != nil {
			return
		}
		go g.handle(conn)
	}
}

func (g *fakeGateway) handle(conn net.Conn) {
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
	lines := strings.Split(string(head), "\r\n")

	g.mu.Lock()
	g.requests = append(g.requests, lines[0])
	for _, l := range lines {
		if strings.HasPrefix(strings.ToLower(l), "proxy-authorization:") {
			g.authz = append(g.authz, strings.TrimSpace(l[len("proxy-authorization:"):]))
		}
	}
	g.mu.Unlock()

	if g.refuse {
		_, _ = conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n"))
		return
	}
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	// Echo the tunnel so the caller can assert bytes flow through.
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (g *fakeGateway) snapshot() ([]string, []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.requests...), append([]string(nil), g.authz...)
}

// startTransparent runs the listener under test against a gateway.
func startTransparent(t *testing.T, gwAddr, basicAuth string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("transparent listen: %v", err)
	}
	go serveTransparent(ln, gwAddr, basicAuth)
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// TestTransparentTunnelsBySNI is the core proof: a client with NO proxy
// configuration dials the listener, and its intended destination — carried
// only in the SNI — reaches the gateway as a credentialed CONNECT.
func TestTransparentTunnelsBySNI(t *testing.T) {
	const wantHost = "agentn.global.api5.cursor.sh"
	creds := base64.StdEncoding.EncodeToString([]byte("x:aoc_test_token"))

	gw := newFakeGateway(t, false)
	addr := startTransparent(t, gw.addr(), creds)

	// A real TLS client. It has no idea it is being proxied: it simply
	// dials and announces its ServerName, exactly like Cursor's ext host.
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tlsConn := tls.Client(conn, &tls.Config{ServerName: wantHost})
	_ = tlsConn.SetDeadline(time.Now().Add(3 * time.Second))
	// Handshake fails: our fake gateway echoes rather than serving TLS.
	// Irrelevant here — what matters is what reached the gateway.
	_ = tlsConn.Handshake()

	deadline := time.Now().Add(3 * time.Second)
	var reqs, auths []string
	for time.Now().Before(deadline) {
		reqs, auths = gw.snapshot()
		if len(reqs) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(reqs) == 0 {
		t.Fatal("gateway saw no CONNECT: the transparent listener did not tunnel")
	}
	wantLine := fmt.Sprintf("CONNECT %s:443 HTTP/1.1", wantHost)
	if reqs[0] != wantLine {
		t.Fatalf("gateway got %q, want %q", reqs[0], wantLine)
	}
	if len(auths) == 0 || auths[0] != "Basic "+creds {
		t.Fatalf("credentials not injected: got %v", auths)
	}
}

// TestTransparentReplaysClientHello proves the sniffed bytes are forwarded,
// not swallowed. Dropping them would break every real TLS handshake.
func TestTransparentReplaysClientHello(t *testing.T) {
	gw := newFakeGateway(t, false)
	addr := startTransparent(t, gw.addr(), "dGVzdA==")

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	hello := captureClientHello(t, "api.anthropic.com")
	if _, err := conn.Write(hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	// The gateway echoes, so the replayed hello must come back verbatim.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	got := make([]byte, len(hello))
	n, err := ioReadFull(conn, got)
	if err != nil {
		t.Fatalf("reading echo after %d bytes: %v", n, err)
	}
	if string(got) != string(hello) {
		t.Fatal("the ClientHello was not replayed byte-for-byte to the gateway")
	}
}

// TestTransparentDropsNonTLS: a redirected connection whose destination
// cannot be recovered must be dropped, never guessed and never sent direct.
func TestTransparentDropsNonTLS(t *testing.T) {
	gw := newFakeGateway(t, false)
	addr := startTransparent(t, gw.addr(), "dGVzdA==")

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: evil.com\r\n\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("non-TLS traffic got a response; it must be dropped")
	}
	if reqs, _ := gw.snapshot(); len(reqs) != 0 {
		t.Fatalf("non-TLS traffic reached the gateway: %v", reqs)
	}
}

// TestTransparentDropsOnGatewayRefusal: if the gateway rejects the CONNECT,
// the client must get a closed connection rather than a broken tunnel that
// looks like a TLS error.
func TestTransparentDropsOnGatewayRefusal(t *testing.T) {
	gw := newFakeGateway(t, true) // refuse
	addr := startTransparent(t, gw.addr(), "dGVzdA==")

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	hello := captureClientHello(t, "api.anthropic.com")
	if _, err := conn.Write(hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	if n, err := conn.Read(buf); err == nil && n > 0 {
		t.Fatalf("got %d bytes after a refused CONNECT; want a closed conn", n)
	}
}

// TestTransparentRejectsSNIHeaderInjection: an SNI carrying CRLF must never
// reach the gateway, or a sandboxed process could forge its own
// Proxy-Authorization on the tunnel.
func TestTransparentRejectsSNIHeaderInjection(t *testing.T) {
	gw := newFakeGateway(t, false)
	addr := startTransparent(t, gw.addr(), "dGVzdA==")

	// Hand-built hello with a malicious SNI; crypto/tls will not emit one.
	evil := buildClientHelloWithSNI("evil.com\r\nProxy-Authorization: Basic AAAA")

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write(evil); err != nil {
		t.Fatalf("write: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	reqs, auths := gw.snapshot()
	if len(reqs) != 0 {
		t.Fatalf("an injected SNI reached the gateway: %v / %v", reqs, auths)
	}
}

// buildClientHelloWithSNI constructs a minimal but structurally valid
// ClientHello carrying an arbitrary server_name, including values that a
// conforming TLS stack would never produce.
func buildClientHelloWithSNI(host string) []byte {
	name := []byte(host)

	var sni []byte
	sni = append(sni, 0x00)                                        // name_type
	sni = append(sni, byte(len(name)>>8), byte(len(name)))         // length
	sni = append(sni, name...)
	var sniList []byte
	sniList = append(sniList, byte(len(sni)>>8), byte(len(sni)))
	sniList = append(sniList, sni...)

	var ext []byte
	ext = append(ext, 0x00, 0x00)                                   // server_name
	ext = append(ext, byte(len(sniList)>>8), byte(len(sniList)))
	ext = append(ext, sniList...)

	var exts []byte
	exts = append(exts, byte(len(ext)>>8), byte(len(ext)))
	exts = append(exts, ext...)

	var body []byte
	body = append(body, 0x03, 0x03)              // client_version
	body = append(body, make([]byte, 32)...)     // random
	body = append(body, 0x00)                    // session_id (empty)
	body = append(body, 0x00, 0x02, 0x13, 0x01)  // cipher_suites
	body = append(body, 0x01, 0x00)              // compression_methods
	body = append(body, exts...)

	var hs []byte
	hs = append(hs, 0x01) // ClientHello
	hs = append(hs, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	hs = append(hs, body...)

	var rec []byte
	rec = append(rec, 0x16, 0x03, 0x01)
	rec = append(rec, byte(len(hs)>>8), byte(len(hs)))
	rec = append(rec, hs...)
	return rec
}

// ioReadFull is io.ReadFull, spelled locally to keep the import list tight.
func ioReadFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
