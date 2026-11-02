//go:build darwin

package lifecycle

import (
	"os"

	"golang.org/x/sys/unix"
)

// Inherit duplicates the open file description, including its flock. Neither
// owner explicitly unlocks it: the last close ends the shared lease.
func (l *Lock) Inherit() (*os.File, error) {
	fd, err := unix.Dup(int(l.file.Fd()))
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), l.file.Name()), nil
}
