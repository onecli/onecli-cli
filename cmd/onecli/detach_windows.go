//go:build windows

package main

// Windows implementations of the detached-child helpers. The features
// that spawn detached helpers (enforce-mode forwarder, pg sidecar) are
// effectively Unix-only today — `onecli run` relies on syscall.Exec,
// which returns EWINDOWS on Windows — but the package must COMPILE for
// windows so goreleaser can ship the other commands.

import (
	"os"
	"syscall"
)

// detachedSysProcAttr: no setsid on Windows. CREATE_NEW_PROCESS_GROUP
// is the closest analogue — the child does not share the console
// Ctrl-C group with the parent.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// processAliveByPID: on Windows os.FindProcess actually opens a handle
// and fails when the process does not exist (unlike Unix, where it
// always succeeds).
func processAliveByPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = proc.Release()
	return true
}
