package main

// Transparent-redirect listener for enforce mode.
//
// The problem it solves: some applications dial remote hosts DIRECTLY,
// ignoring every proxy mechanism we can set from outside — env vars
// (HTTPS_PROXY, NODE_USE_ENV_PROXY), Chromium's --proxy-server, and the
// editor's own settings. Cursor's extension host is the observed case; the
// kernel names it plainly:
//
//	Sandbox: Cursor Helper (Plugin)(81300) deny(1) network-outbound remote:*:443
//
// Configuration-based proxying asks the app to cooperate. This does not:
// a pf anchor rewrites the sandboxed process group's outbound 443 to this
// listener, so a direct dial IS the proxy. The app needs no proxy support,
// and cannot opt out — which is the enforcement guarantee the product sells.
//
// Relationship to the CONNECT forwarder in run_enforce_forwarder.go: that
// one serves clients that DO speak proxy and state their destination in a
// CONNECT line. This one serves clients that state nothing, and recovers the
// destination from the TLS SNI. Both then hand off to the same gateway with
// the same injected credentials.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	// How long to wait for a ClientHello before giving up. A redirected
	// connection that never sends one cannot be routed.
	transparentHelloTimeout = 10 * time.Second
	// The port pf redirects. Only 443 is redirected: plain HTTP is already
	// handled by the proxy env vars, and redirecting everything would catch
	// non-TLS protocols whose destination we cannot recover.
	transparentRedirectPort = 443
)

var errNoHelloTimeout = errors.New("timed out waiting for a TLS ClientHello")

// serveTransparent runs the transparent listener until it is closed.
func serveTransparent(ln net.Listener, upstreamAddr, basicAuth string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleTransparentConn(conn, upstreamAddr, basicAuth)
	}
}

// handleTransparentConn recovers the destination from the SNI, opens a
// credentialed CONNECT tunnel to the gateway, replays the ClientHello, and
// pipes the rest.
//
// Fails CLOSED throughout: a connection whose destination cannot be
// determined is dropped, never guessed at and never sent direct.
func handleTransparentConn(conn net.Conn, upstreamAddr, basicAuth string) {
	defer func() { _ = conn.Close() }()

	// Accept only traffic that pf actually redirected to us.
	//
	// The obvious check — "is the peer 127.0.0.1?" — is WRONG here, and
	// measured to be so: pf's `route-to lo0` preserves the ORIGINAL source
	// address, so a redirected connection arrives from the host's LAN IP
	// (observed: 192.168.99.113) even though it traversed loopback. A
	// loopback-only guard silently rejected every redirected connection
	// while the redirect itself worked perfectly.
	//
	// What actually distinguishes redirected traffic is that our listener
	// is bound to 127.0.0.1, so the kernel only ever delivers connections
	// whose LOCAL address is loopback. Off-machine traffic cannot reach a
	// loopback-bound socket at all. The meaningful check is therefore on
	// the local side, and it is a genuine guard: it fails closed if the
	// listener is ever bound to a wildcard or external address by mistake.
	if la, ok := conn.LocalAddr().(*net.TCPAddr); !ok || !la.IP.IsLoopback() {
		return
	}

	hello, host, err := readClientHello(conn)
	if err != nil {
		return
	}

	upstream, err := net.DialTimeout("tcp", upstreamAddr, enforceDialTimeout)
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()

	// host came from validateSNIHost, so it cannot contain CRLF and cannot
	// forge headers on this connection.
	target := net.JoinHostPort(host, fmt.Sprint(transparentRedirectPort))
	if _, err := fmt.Fprintf(upstream,
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n",
		target, target, basicAuth); err != nil {
		return
	}
	if err := readConnectResponse(upstream); err != nil {
		return
	}

	// Replay the bytes consumed while sniffing, then pipe both ways.
	if _, err := upstream.Write(hello); err != nil {
		return
	}
	go func() {
		_, _ = io.Copy(upstream, conn)
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	_, _ = io.Copy(conn, upstream)
}

// readClientHello reads until a complete ClientHello is buffered, returning
// the raw bytes (for replay) and the parsed SNI host.
func readClientHello(conn net.Conn) ([]byte, string, error) {
	if err := conn.SetReadDeadline(time.Now().Add(transparentHelloTimeout)); err != nil {
		return nil, "", err
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	buf := make([]byte, 0, 2048)
	tmp := make([]byte, 2048)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			host, perr := parseSNI(buf)
			if perr == nil {
				return buf, host, nil
			}
			if !errors.Is(perr, errHelloPartial) {
				// Definitively not routable (not TLS, no SNI, malformed).
				return nil, "", perr
			}
			if len(buf) > maxClientHelloSize {
				return nil, "", errHelloTooBig
			}
			// else: partial, keep reading.
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return nil, "", errNoHelloTimeout
			}
			return nil, "", err
		}
	}
}

// readConnectResponse consumes the gateway's CONNECT reply and verifies it
// succeeded. Without this the client's ClientHello would be written on top
// of an error response and the TLS handshake would fail with a confusing
// protocol error rather than a clean drop.
func readConnectResponse(upstream net.Conn) error {
	if err := upstream.SetReadDeadline(time.Now().Add(enforceDialTimeout)); err != nil {
		return err
	}
	defer func() { _ = upstream.SetReadDeadline(time.Time{}) }()

	// Read byte-wise to the end of the header block: anything buffered past
	// it would belong to the tunnel and must not be swallowed.
	var head []byte
	one := make([]byte, 1)
	for {
		n, err := upstream.Read(one)
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		head = append(head, one[0])
		if len(head) >= 4 && string(head[len(head)-4:]) == "\r\n\r\n" {
			break
		}
		if len(head) > 8192 {
			return fmt.Errorf("CONNECT response header exceeded 8KB")
		}
	}
	if len(head) < 12 || string(head[:5]) != "HTTP/" {
		return fmt.Errorf("malformed CONNECT response")
	}
	// "HTTP/1.1 200 ..." — status begins at offset 9.
	if string(head[9:12]) != "200" {
		return fmt.Errorf("gateway refused CONNECT: %s", string(head[9:12]))
	}
	return nil
}
