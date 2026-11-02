//go:build darwin || linux

package childprocess

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

type processTree struct{ group int }

func prepare(cmd *exec.Cmd) (*processTree, error) {
	attributes := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attributes = *cmd.SysProcAttr
	}
	if attributes.Setsid || attributes.Pgid != 0 {
		return nil, errors.New("owned child cannot use an external process group or session")
	}
	attributes.Setpgid = true
	cmd.SysProcAttr = &attributes
	return &processTree{}, nil
}

func (t *processTree) started(cmd *exec.Cmd) error {
	t.group = cmd.Process.Pid
	return nil
}

func (t *processTree) terminate() error {
	if t.group == 0 {
		return nil
	}
	group := t.group
	err := syscall.Kill(-group, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		t.group = 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("terminate owned process group %d: %w", group, err)
	}
	return err
}
