package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
)

type Lock struct{ file *os.File }

// Installation remains stable while a native profile changes its data directory.
func Installation(root string) (*Lock, error) {
	return Acquire(filepath.Join(root, "installation"))
}

func Acquire(dir string) (*Lock, error) {
	return acquire(dir, true)
}

// Review holds the OS lock while inspecting an unconfirmed lifetime. It does
// not declare the service stopped or remove its ownership refusal marker.
func Review(dir string) (*Lock, error) { return acquire(dir, false) }

func acquire(dir string, requireConfirmed bool) (*Lock, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("device data directory is already in use: %w", err)
	}
	if requireConfirmed {
		if err = confirmOwnership(dir); err != nil {
			file.Close()
			return nil, err
		}
	}
	return &Lock{file: file}, nil
}
func (l *Lock) Close() error { return l.file.Close() }
