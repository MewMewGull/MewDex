//go:build !darwin

package processidentity

import "syscall"

func Lookup(int) (Identity, error)          { return Identity{}, ErrUnsupported }
func ListPIDs() ([]int, error)              { return nil, ErrUnsupported }
func Signal(Identity, syscall.Signal) error { return ErrUnsupported }
