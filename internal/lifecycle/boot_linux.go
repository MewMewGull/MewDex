//go:build linux

package lifecycle

import "os"

func nativeBootSession() (string, error) {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return string(raw), err
}
