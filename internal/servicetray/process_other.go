//go:build !windows

package servicetray

import "os/exec"

func detach(*exec.Cmd) {}
