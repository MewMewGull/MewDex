//go:build !windows && !darwin

package servicetray

import (
	"errors"
	"os/exec"
)

func OpenURL(address string) error { return exec.Command("xdg-open", address).Run() }
func RunOpenHelper() error         { return errors.New("the URL helper is macOS only") }

func ShowError(title, message string) {}
