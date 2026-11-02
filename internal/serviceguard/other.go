//go:build !darwin

package serviceguard

import (
	"context"
	"errors"

	"mewdex/internal/lifecycle"
)

func monitor(ctx context.Context, _ []*lifecycle.Lock) (context.Context, func() error, error) {
	return ctx, func() error { return nil }, nil
}

func Dispatch(_ []string) error {
	return errors.New("internal process supervision is unsupported on this platform")
}
