//go:build !windows

package hubtray

import "context"

func Start(context.Context, string, context.CancelFunc) func() { return func() {} }
