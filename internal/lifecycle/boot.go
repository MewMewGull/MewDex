package lifecycle

import (
	"encoding/hex"
	"errors"
	"strings"
)

var ErrBootUnavailable = errors.New("the operating system boot session is unavailable")

// BootSession identifies the OS kernel lifetime. It is never estimated from
// wall-clock time, uptime, a login session or the application's own lifetime.
// Sleeping and resuming the same OS cannot authorize crash recovery.
func BootSession() (string, error) {
	value, err := nativeBootSession()
	if err != nil {
		return "", err
	}
	return NormalizeBootSession(value)
}

func NormalizeBootSession(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", ErrBootUnavailable
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(raw) != 16 || strings.Trim(strings.ReplaceAll(value, "-", ""), "0") == "" {
		return "", ErrBootUnavailable
	}
	return value, nil
}
