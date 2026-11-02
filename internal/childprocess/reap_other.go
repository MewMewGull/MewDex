//go:build !darwin

package childprocess

func retryKillAfterReap(error) bool { return false }
