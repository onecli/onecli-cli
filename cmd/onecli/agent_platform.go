package main

import (
	"runtime"

	"github.com/onecli/onecli-cli/pkg/exitcode"
)

// goos is runtime.GOOS, a var so tests can act as another platform.
var goos = runtime.GOOS

// msgWindowsUnsupported is shown by connect, signup, exec and run on Windows.
// The CA bundle those commands write needs the system root certificates as
// PEM, and Go cannot export the Windows root store, so they stop before any
// call to the server.
const msgWindowsUnsupported = "Connecting an agent on Windows is not supported yet. Use WSL."

// checkPlatform returns an error on platforms where an agent cannot connect.
func checkPlatform() error {
	if goos == "windows" {
		return &agentError{code: exitcode.CodeError, exit: exitcode.Error,
			msg:    msgWindowsUnsupported,
			action: "install WSL (https://learn.microsoft.com/windows/wsl/install) and run onecli inside it"}
	}
	return nil
}
