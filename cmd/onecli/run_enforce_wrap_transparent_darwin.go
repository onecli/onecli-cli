//go:build darwin

package main

// Wiring transparent redirect into `onecli run --enforce`.
//
// Opt-in via ONECLI_ENFORCE_TRANSPARENT=1 rather than on by default. The
// default wrap path denies direct egress at the OS layer and needs no
// privileged setup; transparent mode trades that for pf-layer governance so
// apps that ignore proxy configuration can still be governed. That is the
// right trade for a GUI editor and the wrong one for everything else, so the
// caller states it explicitly.
//
// The two artifacts must be installed together or not at all: the widened
// Seatbelt profile is only safe while the anchor is loaded. resolveEnforce-
// WrapTransparent either returns both or returns an error.

import (
	"fmt"
	"os"

	"github.com/onecli/onecli-cli/internal/sandbox"
)

// enforceTransparentEnv opts a run into transparent redirect.
const enforceTransparentEnv = "ONECLI_ENFORCE_TRANSPARENT"

// transparentRequested reports whether the caller asked for transparent mode.
func transparentRequested() bool {
	return os.Getenv(enforceTransparentEnv) == "1"
}

// resolveEnforceWrapTransparent prepares wrap mode WITH transparent redirect.
//
// Returns the sandbox profile, the CONNECT forwarder port (still used by
// apps that DO honor proxy env), and the live session owning the pf anchor.
// The caller must Close the session when the run ends.
//
// Fails closed: any error leaves nothing installed. In particular the
// widened profile is only materialized AFTER the anchor is verified live,
// so a failure can never leave a profile permitting direct 443 with no pf
// rules behind it.
func resolveEnforceWrapTransparent(env map[string]string) (
	profilePath string, port uint16, sess *transparentSession, err error,
) {
	if err := sandbox.Available(); err != nil {
		return "", 0, nil, err
	}
	gatewayURL := firstProxyURL(env)
	if gatewayURL == "" {
		return "", 0, nil, fmt.Errorf("no gateway proxy URL in the resolved environment")
	}

	// The transparent session verifies setup, binds its listener, and loads
	// the anchor. It refuses if any precondition is missing.
	sess, err = startTransparentSession(gatewayURL)
	if err != nil {
		return "", 0, nil, fmt.Errorf("transparent redirect unavailable: %w", err)
	}

	// The CONNECT forwarder still runs: apps that honor proxy env should
	// use it directly rather than taking the redirect path, and Chromium's
	// main process already does.
	port, err = spawnEnforceForwarder(gatewayURL)
	if err != nil {
		_ = sess.Close()
		return "", 0, nil, err
	}
	rewriteProxyEnvToLoopback(env, port)

	// Only now, with the anchor verified live, is the widened profile safe.
	profilePath, err = sandbox.MaterializeOpts(sandbox.Options{
		ForwarderPort: port,
		Transparent:   true,
	})
	if err != nil {
		_ = sess.Close()
		return "", 0, nil, err
	}
	return profilePath, port, sess, nil
}

// transparentWrapArgv prefixes the sandbox launcher with the setgid helper
// so the confined tree runs under the group pf redirects.
//
// Order is load-bearing and was verified both ways:
//
//	helper -> sandbox-exec -> cmd    gid 700, works
//	sandbox-exec -> helper -> cmd    execvp: Operation not permitted
//
// Seatbelt refuses to exec a setgid binary from inside the sandbox, so the
// helper must run OUTSIDE it. The resulting group is inherited by the whole
// confined tree, which is what pf matches.
func transparentWrapArgv(argv []string) []string {
	return append([]string{setgidHelperPath}, argv...)
}
