//go:build darwin

package lifecycle

import "golang.org/x/sys/unix"

func nativeBootSession() (string, error) { return unix.Sysctl("kern.bootsessionuuid") }
