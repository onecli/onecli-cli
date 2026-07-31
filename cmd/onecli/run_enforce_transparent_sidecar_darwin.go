//go:build darwin

package main

// Detached sidecar for the transparent listener.
//
// Why this exists: `onecli run` ends in syscall.Exec, which REPLACES the
// process image with the agent. An in-process listener goroutine dies at
// that moment while the pf anchor survives, so every redirected connection
// hits a dead port. Observed exactly that way: gid=700 proved the group was
// adopted and curl failed in 2ms, the signature of a loopback RST.
//
// The CONNECT forwarder already solved this by forking a detached child that
// inherits the bound listener as FD 3. This mirrors that pattern rather than
// inventing a second lifecycle model.

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

const (
	transparentSidecarFlag = "__enforce-transparent"
	// Carries the gateway proxy URL (with credentials) to the sidecar via
	// env, not argv: argv is world-readable in `ps`.
	transparentProxyURLEnv = "ONECLI_TRANSPARENT_PROXY_URL"
)

// spawnTransparentListener binds the listener, forks a detached sidecar
// holding it, and returns the port. Must be called BEFORE syscall.Exec.
func spawnTransparentListener(gatewayProxyURL string) (uint16, error) {
	if _, _, err := parseGatewayProxy(gatewayProxyURL); err != nil {
		return 0, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("binding transparent listener: %w", err)
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
	defer func() {
		_ = lnFile.Close()
		_ = ln.Close()
	}()

	self, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolving own binary: %w", err)
	}
	cmd := exec.Command(self, transparentSidecarFlag,
		"--parent-pid", strconv.Itoa(os.Getpid()))
	cmd.Env = append(os.Environ(), transparentProxyURLEnv+"="+gatewayProxyURL)
	cmd.ExtraFiles = []*os.File{lnFile} // FD 3 in the child
	cmd.SysProcAttr = detachedSysProcAttr()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting transparent listener: %w", err)
	}
	_ = cmd.Process.Release()
	return port, nil
}

// parseTransparentSidecarArgs recognizes the hidden sidecar invocation.
func parseTransparentSidecarArgs(argv []string) (parentPID int, ok bool) {
	if len(argv) < 1 || argv[0] != transparentSidecarFlag {
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

// runTransparentSidecar serves the inherited listener until the agent exits.
func runTransparentSidecar(parentPID int) {
	proxyURL := os.Getenv(transparentProxyURLEnv)
	upstreamAddr, basicAuth, err := parseGatewayProxy(proxyURL)
	if err != nil {
		return
	}
	lnFile := os.NewFile(3, "onecli-transparent-listener")
	if lnFile == nil {
		return
	}
	ln, err := net.FileListener(lnFile)
	if err != nil {
		return
	}
	defer func() { _ = ln.Close() }()

	// The sidecar owns anchor cleanup, because nothing else can.
	//
	// `onecli run` ends in syscall.Exec, which REPLACES the process image
	// with the agent, so the parent's deferred Close and signal handlers
	// never run. Measured: after an enforced run ended, the sidecar exited
	// correctly but the anchor persisted indefinitely (rdr=1 at t+10s),
	// leaving group 700 redirected to a dead port. That would break the
	// NEXT enforced run in a confusing way.
	//
	// The sidecar is the only component that outlives the exec and knows
	// when the agent is gone, so it flushes the anchor before exiting.
	cleanup := func(code int) {
		if err := pfFlushAnchor(); err != nil {
			// Nothing useful to do with the error here: stderr is
			// detached. Exiting without flushing is the worse outcome, so
			// the attempt is unconditional and best-effort.
			_ = err
		}
		os.Exit(code)
	}

	go func() {
		for {
			time.Sleep(enforceParentPollTime)
			if !enforceProcessAlive(parentPID) {
				cleanup(0)
			}
		}
	}()

	// Signals too: a killed sidecar must not leave the anchor behind.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigCh
		cleanup(130)
	}()

	serveTransparent(ln, upstreamAddr, basicAuth)
}
