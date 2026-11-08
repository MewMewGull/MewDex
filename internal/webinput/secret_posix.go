//go:build !windows

package webinput

// The connection stays in the input's user-only 0700 directory with mode 0600.
func protect(raw []byte) ([]byte, error)   { return raw, nil }
func unprotect(raw []byte) ([]byte, error) { return raw, nil }
