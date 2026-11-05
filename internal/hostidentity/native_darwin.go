//go:build darwin

package hostidentity

import (
	"context"
	"errors"
	"mewdex/internal/childprocess"
	"os/exec"
	"regexp"
	"time"
)

func nativeID(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
	raw, err := childprocess.OutputContext(ctx, command, 64<<10)
	if err != nil {
		return "", err
	}
	match := regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([A-Fa-f0-9-]+)"`).FindSubmatch(raw)
	if len(match) != 2 {
		return "", errors.New("macOS platform identity is unavailable")
	}
	return string(match[1]), nil
}
