//go:build !windows

package main

// Unix implementations of the detached-child helpers shared by the
// enforce-mode forwarder and the pg session sidecar.

import "syscall"

// detachedSysProcAttr detaches a spawned helper from this process's
// session (setsid), so it survives the parent's syscall.Exec image
// replacement and never holds the controlling TTY.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// processAliveByPID reports whether pid is still running. Signal 0
// probes without delivering; EPERM means it exists but belongs to
// another user — still alive.
func processAliveByPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
