package startup

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"mewdex/internal/childprocess"
)

func run(ctx context.Context, program string, args []string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, program, args...)
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	return childprocess.Output(command, 1<<20)
}
