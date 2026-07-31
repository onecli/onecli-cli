//go:build !darwin

package main

// Non-darwin builds have no transparent redirect: it depends on pf, which is
// macOS-specific. The type exists so run.go compiles unchanged; the Linux
// equivalent (nftables REDIRECT in a netns) is a separate piece of work.

type transparentSession struct{}

func (s *transparentSession) Close() error { return nil }

// setgidHelperPath has no non-darwin equivalent yet; transparentSess is
// always nil here so it is never used.
const setgidHelperPath = ""

func transparentWrapArgv(argv []string) []string { return argv }

// The transparent sidecar has no non-darwin implementation; these keep
// main.go free of build tags.
func parseTransparentSidecarArgs([]string) (int, bool) { return 0, false }

func runTransparentSidecar(int) {}

func resolveEnforceWrapMode(env map[string]string) (
	profilePath string, port uint16, sess *transparentSession, err error,
) {
	profilePath, port, err = resolveEnforceWrap(env)
	return profilePath, port, nil, err
}
