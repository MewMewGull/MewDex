//go:build !darwin && !linux && !windows

package lifecycle

func nativeBootSession() (string, error) { return "", ErrBootUnavailable }
