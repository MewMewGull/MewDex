//go:build !windows && !darwin

package servicetray

import (
	"context"
	"errors"
)

func Start(context.Context, Options) (func(), error) { return func() {}, nil }
func RunHelper() error                               { return errors.New("native tray helper is unavailable on this platform") }
