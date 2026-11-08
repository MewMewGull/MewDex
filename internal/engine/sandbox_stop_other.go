//go:build !darwin && !linux

package engine

import "os/exec"

func configureNativeSandboxStop(command *exec.Cmd) {}

func (s *Sandbox) UnconfirmedExit(command *exec.Cmd) bool { return false }
