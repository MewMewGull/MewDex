//go:build darwin || linux

package engine

import (
	"os/exec"
	"syscall"
	"time"
)

func configureNativeSandboxStop(command *exec.Cmd) {
	// Native POSIX hosts acknowledge descendant cleanup before exiting.
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	// Cover the five-second MXC or Linux subreaper shutdown budget.
	command.WaitDelay = 6 * time.Second
}

func (s *Sandbox) UnconfirmedExit(command *exec.Cmd) bool {
	if s.nativeVersion == "" || command.ProcessState == nil {
		return false
	}
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	return ok && status.Signaled()
}
