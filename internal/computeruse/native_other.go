//go:build !windows

package computeruse

import (
	"context"
	"errors"

	"mewdex/internal/protocol"
)

const unsupported = "该系统的原生 Computer Use 驱动尚未实现"

func prepareNative() (func(), error) { return func() {}, nil }

func (d *nativeDriver) Available() (bool, string) { return false, unsupported }

func (d *nativeDriver) Reserve(context.Context) (Reservation, error) {
	return nil, errors.New(unsupported)
}

func nativeWindows() ([]protocol.ComputerWindow, error) { return nil, errors.New(unsupported) }

func nativeObserve(protocol.ComputerWindow) (protocol.ComputerObservation, Bounds, error) {
	return protocol.ComputerObservation{}, Bounds{}, errors.New(unsupported)
}

func nativeAct(nativeRequest) error { return errors.New(unsupported) }
