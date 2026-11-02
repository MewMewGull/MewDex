//go:build !windows && !darwin && !linux

package childprocess

import (
	"errors"
	"os/exec"
)

type processTree struct{}

func prepare(*exec.Cmd) (*processTree, error) {
	return nil, errors.New("owned child processes are unsupported on this platform")
}
func (*processTree) started(*exec.Cmd) error { return nil }
func (*processTree) terminate() error        { return nil }
