//go:build darwin

package childprocess

import (
	"errors"
	"syscall"
)

func retryKillAfterReap(err error) bool { return errors.Is(err, syscall.EPERM) }
