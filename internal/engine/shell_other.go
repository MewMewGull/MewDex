//go:build !windows

package engine

func nativeEngineShell() (string, error) { return "", nil }

func nativeEngineSearchPath(original string) string { return original }
