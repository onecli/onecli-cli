package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/onecli/onecli-cli/internal/agentenv"
	"github.com/onecli/onecli-cli/pkg/exitcode"
)

// On Windows, the commands that need a CA bundle stop before any call to
// the server and write nothing.
func TestWindowsRefusesToConnect(t *testing.T) {
	cases := map[string]func() error{
		"connect": func() error {
			out, _, _ := newOut()
			return (&ConnectCmd{Token: testToken, Name: "bot"}).Run(out)
		},
		"signup": func() error {
			out, _, _ := newOut()
			return (&SignupCmd{Name: "bot"}).Run(out)
		},
		"exec": func() error {
			out, _, _ := newOut()
			return (&ExecCmd{Args: []string{"--", "true"}}).Run(out)
		},
		"run": func() error {
			out, _, _ := newOut()
			return (&RunCmd{Args: []string{"true"}}).Run(out)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			_, dir := agentEnv(t)
			f := newFakeAPI(t)
			orig := goos
			goos = "windows"
			t.Cleanup(func() { goos = orig })

			e := wantAgentErr(t, run(), exitcode.CodeError, exitcode.Error)
			if e.msg != msgWindowsUnsupported {
				t.Fatalf("msg = %q", e.msg)
			}
			if n := len(f.calls); n != 0 {
				t.Fatalf("%d calls to the server, want 0", n)
			}
			if _, err := os.Stat(filepath.Join(dir, agentenv.FileName)); !os.IsNotExist(err) {
				t.Fatalf("agent.env exists (err = %v)", err)
			}
		})
	}
}

func TestCheckPlatformAllowsUnix(t *testing.T) {
	orig := goos
	t.Cleanup(func() { goos = orig })
	for _, g := range []string{"darwin", "linux", "freebsd"} {
		goos = g
		if err := checkPlatform(); err != nil {
			t.Fatalf("%s: %v", g, err)
		}
	}
}
