// Package serviceguard retains service leases through descendant cleanup. On
// Darwin a separate helper owns birth identities; other platforms retain their
// existing Job/systemd process control without a second supervisor.
package serviceguard

import (
	"context"
	"errors"
	"path/filepath"

	"mewdex/internal/lifecycle"
)

type scopeKey struct{}
type scope struct {
	directory string
	protected bool
}

// Run is shared by OS-managed and direct service entries. A managed callback
// may enter Run again, but cannot substitute a different data directory or add
// execution after choosing an unprotected service.
func Run(ctx context.Context, directory string, installation *lifecycle.Lock, protect bool, action func(context.Context) error) (result error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if previous, ok := ctx.Value(scopeKey{}).(scope); ok {
		if previous.directory != directory || protect && !previous.protected || installation != nil {
			return errors.New("service ownership scope cannot change inside a managed callback")
		}
		return action(ctx)
	}
	data, err := lifecycle.Acquire(directory)
	if err != nil {
		return err
	}
	defer data.Close()
	leases := []*lifecycle.Lock{data}
	if installation != nil {
		leases = append(leases, installation)
	}
	ctx = context.WithValue(ctx, scopeKey{}, scope{directory: directory, protected: protect})
	finish := func() error { return nil }
	if protect {
		ctx, finish, err = monitor(ctx, leases)
		if err != nil {
			return err
		}
	}
	defer func() { result = errors.Join(result, finish()) }()
	return action(ctx)
}
