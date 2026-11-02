// Package childprocess owns a launched process and its inherited descendants.
// It is independent of the model protocol and never searches for unrelated PIDs.
package childprocess

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Process struct {
	tree     *processTree
	done     chan struct{}
	reaped   chan struct{}
	waitErr  error
	killErr  error
	killOnce sync.Once
}

// Start owns cmd.Wait. Streaming callers must use their own os.Pipe rather than
// Cmd.StdoutPipe, whose read end Wait closes before its consumer has drained it.
func Start(cmd *exec.Cmd) (*Process, error) {
	return StartContext(context.Background(), cmd)
}

func StartContext(ctx context.Context, cmd *exec.Cmd) (*Process, error) {
	confirm := func() error { return nil }
	if guard, ok := ctx.Value(guardKey{}).(LaunchGuard); ok {
		var release func()
		var err error
		confirm, release, err = guard.Prepare(ctx, cmd)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	tree, err := prepare(cmd)
	if err != nil {
		return nil, err
	}
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 2 * time.Second
	}
	if err = cmd.Start(); err != nil {
		_ = tree.terminate()
		return nil, err
	}
	if err = tree.started(cmd); err != nil {
		_ = tree.terminate()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	if err = confirm(); err != nil {
		_ = tree.terminate()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	p := &Process{tree: tree, done: make(chan struct{}), reaped: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.reaped)
		_ = p.Kill() // Also remove descendants after the main process exits itself.
		close(p.done)
	}()
	return p, nil
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) Kill() error {
	p.killOnce.Do(func() {
		p.killErr = p.tree.terminate()
		if retryKillAfterReap(p.killErr) {
			// Darwin may report EPERM for an exited, unreaped group leader.
			// Reap only our child, then signal the SAME owned group again.
			// A live descendant or a real denial must still return its error.
			timer := time.NewTimer(500 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-p.reaped:
				p.killErr = p.tree.terminate()
			case <-timer.C:
			}
		}
	})
	return p.killErr
}

func (p *Process) Wait() error {
	<-p.done
	return errors.Join(p.waitErr, p.killErr)
}

// Output captures bounded stdout without a copy goroutine that could keep Wait
// blocked when a descendant inherits an output pipe. Context belongs to cmd.
func Output(cmd *exec.Cmd, limit int64) ([]byte, error) {
	return OutputContext(context.Background(), cmd, limit)
}

func OutputContext(ctx context.Context, cmd *exec.Cmd, limit int64) ([]byte, error) {
	if limit <= 0 || limit > 64<<20 || cmd.Stdout != nil {
		return nil, errors.New("choose a positive output limit and an unused stdout")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	cmd.Stdout = writer
	p, err := StartContext(ctx, cmd)
	_ = writer.Close()
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
	if len(raw) > int(limit) {
		_ = p.Kill()
		_ = p.Wait()
		return nil, errors.New("child output exceeded its limit")
	}
	if readErr != nil {
		_ = p.Kill()
	}
	return raw, errors.Join(readErr, p.Wait())
}
