//go:build !linux

package linuxuserns

import (
	"context"
	"errors"
)

func Environment(environment []string) ([]string, error) { return environment, nil }
func Guidance() string                                   { return "" }
func Inspect(string) (Plan, error) {
	return Plan{}, errors.New("user namespace preparation is available on Linux")
}
func Apply(context.Context, string, string, bool) (Plan, error) {
	return Plan{}, errors.New("user namespace preparation is available on Linux")
}
