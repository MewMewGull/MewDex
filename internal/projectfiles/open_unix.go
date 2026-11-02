//go:build !windows

package projectfiles

import (
	"os"
	"syscall"
)

func openRead(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
