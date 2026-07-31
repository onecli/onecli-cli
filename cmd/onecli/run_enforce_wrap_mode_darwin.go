//go:build darwin

package main

// resolveEnforceWrapMode picks between the default wrap path and transparent
// redirect, so run.go has one call site and no build tags.

// setgidHelperPath is the installed helper that adopts the sandbox group.
// Fixed, never resolved through PATH: a shadowed helper must not become the
// mechanism that decides whether traffic is governed.
const setgidHelperPath = "/usr/local/bin/onecli-sandbox-gid"

func resolveEnforceWrapMode(env map[string]string) (
	profilePath string, port uint16, sess *transparentSession, err error,
) {
	if transparentRequested() {
		return resolveEnforceWrapTransparent(env)
	}
	profilePath, port, err = resolveEnforceWrap(env)
	return profilePath, port, nil, err
}
