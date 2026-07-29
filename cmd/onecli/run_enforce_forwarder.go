package main

// The enforce-mode auth forwarder: a tiny loopback proxy that lets Claude
// Code's OS sandbox (Seatbelt/bubblewrap) route ALL sandboxed Bash egress
// through the OneCLI gateway.
//
// Why it exists: the sandbox's `network.httpProxyPort` setting takes a bare
// localhost port with no credential support, while the gateway requires the
// agent's aoc_ token as proxy credentials. This forwarder bridges the two:
// it accepts unauthenticated connections from loopback ONLY, injects
// `Proxy-Authorization`, and pipes bytes to the real gateway.
//
// Lifecycle mirrors the exec model of `onecli run`: run.go execs the agent
// in-place (same PID), so the forwarder is forked FIRST as a detached
// child holding the pre-bound listener FD, watches its parent PID (which
// becomes the agent after exec), and exits when the agent does.

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const (
	enforceForwarderFlag = "__enforce-forwarder"
	// Env var carrying the gateway proxy URL (with credentials) to the
	// forwarder child. Env, not argv: argv is world-readable in `ps`.
	enforceProxyURLEnv    = "ONECLI_ENFORCE_PROXY_URL"
	enforceParentPollTime = 2 * time.Second
	enforceDialTimeout    = 10 * time.Second
)

// spawnEnforceForwarder binds a loopback listener, forks a detached copy of
// this binary in forwarder mode (passing the listener as FD 3), and returns
// the bound port. Must be called BEFORE syscall.Exec. Unlike best-effort
// helpers, a failure here is returned: --enforce promises enforcement, and
// a missing forwarder would mean the sandbox has no route to the gateway.
func spawnEnforceForwarder(gatewayProxyURL string) (uint16, error) {
	if gatewayProxyURL == "" {
		return 0, fmt.Errorf("no gateway proxy URL in the resolved environment")
	}
	if _, _, err := parseGatewayProxy(gatewayProxyURL); err != nil {
		return 0, err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("binding forwarder listener: %w", err)
	}
	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		_ = ln.Close()
		return 0, fmt.Errorf("unexpected listener type %T", ln)
	}
	port := uint16(tcpLn.Addr().(*net.TCPAddr).Port)

	lnFile, err := tcpLn.File()
	if err != nil {
		_ = ln.Close()
		return 0, fmt.Errorf("duplicating listener fd: %w", err)
	}
	// The child holds its dup; the parent's copies close so the exec'd
	// agent does not inherit a stray listener.
	defer func() {
		_ = lnFile.Close()
		_ = ln.Close()
	}()

	self, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolving own binary: %w", err)
	}

	cmd := exec.Command(self, enforceForwarderFlag,
		"--parent-pid", strconv.Itoa(os.Getpid()))
	cmd.Env = append(os.Environ(), enforceProxyURLEnv+"="+gatewayProxyURL)
	cmd.ExtraFiles = []*os.File{lnFile} // becomes FD 3 in the child
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = detachedSysProcAttr()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting forwarder: %w", err)
	}
	// Detach fully: the forwarder outlives this process image.
	_ = cmd.Process.Release()
	return port, nil
}

// parseGatewayProxy splits a gateway proxy URL (http://x:aoc_...@host:port)
// into the upstream dial address and the Basic credentials to inject.
func parseGatewayProxy(raw string) (hostPort, basicAuth string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parsing gateway proxy URL: %w", err)
	}
	if u.Host == "" || u.User == nil {
		return "", "", fmt.Errorf("gateway proxy URL carries no host or credentials")
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "80")
	}
	pass, _ := u.User.Password()
	creds := u.User.Username() + ":" + pass
	return host, base64.StdEncoding.EncodeToString([]byte(creds)), nil
}

// parseEnforceForwarderArgs recognizes the hidden sidecar invocation.
func parseEnforceForwarderArgs(argv []string) (parentPID int, ok bool) {
	if len(argv) < 1 || argv[0] != enforceForwarderFlag {
		return 0, false
	}
	for i := 1; i < len(argv)-1; i++ {
		if argv[i] == "--parent-pid" {
			pid, err := strconv.Atoi(argv[i+1])
			if err != nil {
				return 0, false
			}
			return pid, true
		}
	}
	return 0, false
}

// runEnforceForwarder is the sidecar entrypoint: serve the inherited
// listener until the parent (the agent) exits.
func runEnforceForwarder(parentPID int) {
	proxyURL := os.Getenv(enforceProxyURLEnv)
	upstreamAddr, basicAuth, err := parseGatewayProxy(proxyURL)
	if err != nil {
		return
	}

	lnFile := os.NewFile(3, "onecli-enforce-listener")
	if lnFile == nil {
		return
	}
	ln, err := net.FileListener(lnFile)
	if err != nil {
		return
	}
	defer func() { _ = ln.Close() }()

	// Parent watcher: when the agent exits, so does the forwarder.
	go func() {
		for {
			time.Sleep(enforceParentPollTime)
			if !enforceProcessAlive(parentPID) {
				os.Exit(0)
			}
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go forwardConn(conn, upstreamAddr, basicAuth)
	}
}

// forwardConn bridges one sandbox connection to the gateway, injecting
// proxy credentials. CONNECT becomes a raw byte tunnel after the header
// swap; plain HTTP requests are forced to one-per-connection so every
// request on the wire carries the credential.
func forwardConn(conn net.Conn, upstreamAddr, basicAuth string) {
	defer func() { _ = conn.Close() }()

	// Loopback-only: refuse anything that is not 127.0.0.1/::1. The
	// listener binds loopback already; this guards against fd confusion.
	if ra, ok := conn.RemoteAddr().(*net.TCPAddr); !ok || !ra.IP.IsLoopback() {
		return
	}

	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}

	upstream, err := net.DialTimeout("tcp", upstreamAddr, enforceDialTimeout)
	if err != nil {
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer func() { _ = upstream.Close() }()

	if req.Method == http.MethodConnect {
		_, err = fmt.Fprintf(upstream,
			"CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n",
			req.Host, req.Host, basicAuth)
		if err != nil {
			return
		}
	} else {
		req.Header.Set("Proxy-Authorization", "Basic "+basicAuth)
		// One request per connection: a keep-alive follow-up would ride
		// the raw pipe below without credentials.
		req.Header.Set("Connection", "close")
		req.Close = true
		if err := req.WriteProxy(upstream); err != nil {
			return
		}
	}

	// Pump both directions with half-close semantics: when the client
	// finishes sending, propagate EOF to the gateway (CloseWrite) but keep
	// reading the response. Return only when the response stream ends —
	// tearing down on the first finished direction would truncate
	// responses to clients that half-close after the request.
	go func() {
		_, _ = io.Copy(upstream, br)
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	_, _ = io.Copy(conn, upstream)
}

// enforceProcessAlive reports whether pid is still running.
func enforceProcessAlive(pid int) bool {
	return processAliveByPID(pid)
}
