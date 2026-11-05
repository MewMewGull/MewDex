//go:build !windows && !linux && !darwin

package hostidentity

import (
	"context"
	"errors"
)

func nativeID(context.Context) (string, error) {
	return "", errors.New("this platform does not support an execution host identity")
}
