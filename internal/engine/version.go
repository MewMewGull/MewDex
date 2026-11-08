package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/config"
)

// ReadVersion inspects a configured executable without opening a model session
// or creating an account home. Migration validates the resulting version.
func ReadVersion(ctx context.Context, settings config.Engine) (string, error) {
	program, arguments, err := Resolve(settings)
	if err != nil {
		return "", err
	}
	return executableVersion(ctx, program, arguments)
}

func executableVersion(ctx context.Context, program string, arguments []string) (string, error) {
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := append(append([]string{}, arguments...), "--version")
	command := exec.CommandContext(check, program, args...)
	command.Env = independentEngineEnvironment(os.Environ())
	output, err := childprocess.OutputContext(check, command, 64<<10)
	if err != nil {
		return "", errors.New("cannot determine local runtime version")
	}
	return strings.TrimSpace(string(output)), nil
}
