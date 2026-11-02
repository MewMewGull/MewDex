//go:build !windows

package serviceport

import (
	"errors"
	"syscall"
)

func occupied(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
