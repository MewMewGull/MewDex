package localnode

import (
	"errors"
	"os"
)

// OpenServiceLog opens only this installation's regular append-only log.
// Call before registering an OS entry so launchd does not create a public file.
func OpenServiceLog(directory string) (*os.File, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if info, statErr := root.Lstat("service.log"); statErr == nil && !info.Mode().IsRegular() {
		return nil, errors.New("service log must be a regular file")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	return root.OpenFile("service.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
}
