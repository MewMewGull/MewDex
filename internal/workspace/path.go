// Package workspace defines shared directory identity and scheduling boundaries.
package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func CanonicalDir(path string) (string, error) {
	if path == "" {
		return "", errors.New("workspace directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("workspace must be a directory")
	}
	return filepath.Clean(resolved), nil
}

// Overlap is a conservative scheduling test, not an access-control decision.
// Inputs must already be canonical. Case folding on desktop filesystems can
// serialize two distinct case-sensitive directories; it cannot grant access.
func Overlap(a, b string) bool {
	if a == "" || b == "" {
		return true
	} // An unknown old root cannot be ignored.
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	within := func(root, target string) bool {
		relative, err := filepath.Rel(root, target)
		return err == nil && filepath.IsLocal(relative)
	}
	return within(a, b) || within(b, a)
}
