//go:build windows

package localnode

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

func detach(command *exec.Cmd) {
	// A device service outlives the input window and its process job.
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_BREAKAWAY_FROM_JOB}
}
