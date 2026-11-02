//go:build !windows && !linux && !darwin

package startup

import "errors"

func Native() (Backend, error) {
	return nil, errors.New("automatic device startup is unsupported on this platform")
}
