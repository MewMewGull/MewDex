//go:build windows

package serviceport

import (
	"errors"
	"golang.org/x/sys/windows"
)

func occupied(err error) bool { return errors.Is(err, windows.WSAEADDRINUSE) }
