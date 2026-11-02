package childprocess

import (
	"context"
	"os/exec"
)

// LaunchGuard can stage a newly created child before it executes its payload.
// confirm runs after Start, before the process is returned to its caller.
// release must restore command metadata and close staging descriptors on every
// path, including preparation or OS launch failure.
type LaunchGuard interface {
	Prepare(context.Context, *exec.Cmd) (confirm func() error, release func(), err error)
}

type guardKey struct{}

func WithLaunchGuard(ctx context.Context, guard LaunchGuard) context.Context {
	return context.WithValue(ctx, guardKey{}, guard)
}
