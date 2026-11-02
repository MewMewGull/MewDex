//go:build darwin

package serviceguard

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/lifecycle"
	"mewdex/internal/processidentity"
	"mewdex/internal/protocol"
)

const messageLimit = 1 << 20

type startMessage struct {
	ID          string                   `json:"id"`
	Owner       processidentity.Identity `json:"owner"`
	Directories []string                 `json:"directories"`
}
type request struct {
	Operation string                   `json:"operation"`
	Identity  processidentity.Identity `json:"identity,omitempty"`
}
type reply struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}
type launchMessage struct {
	Path      string   `json:"path"`
	Arguments []string `json:"arguments"`
}

type client struct {
	mu      sync.Mutex
	input   *os.File
	output  *os.File
	replies chan reply
	done    chan struct{}
	wait    chan error
	cancel  context.CancelFunc
	program string
}

func send(file *os.File, value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > messageLimit {
		return errors.New("process ownership message exceeds its bound")
	}
	_, err = file.Write(append(raw, '\n'))
	return err
}

func scanner(file *os.File) *bufio.Scanner {
	result := bufio.NewScanner(file)
	result.Buffer(make([]byte, 4096), messageLimit)
	return result
}

func (c *client) receive() {
	defer close(c.done)
	defer c.cancel() // An unexpected monitor exit stops the service's work.
	input := scanner(c.output)
	for input.Scan() {
		var value reply
		if json.Unmarshal(input.Bytes(), &value) != nil {
			return
		}
		select {
		case c.replies <- value:
		default:
			return // Unsolicited/unbounded responses cannot block supervision.
		}
	}
}

func (c *client) response(want string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case value := <-c.replies:
		if value.Error != "" || value.State != want {
			return fmt.Errorf("process monitor did not confirm %s: %s", want, value.Error)
		}
		return nil
	case <-c.done:
		// The final reply may already be queued when the process closes its pipe.
		select {
		case value := <-c.replies:
			if value.State == want && value.Error == "" {
				return nil
			}
		default:
		}
		return lifecycle.ErrUnconfirmed
	case <-timer.C:
		return errors.Join(lifecycle.ErrUnconfirmed, errors.New("process monitor response timed out"))
	}
}

func (c *client) call(value request, want string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.input.SetWriteDeadline(time.Now().Add(8 * time.Second)); err != nil {
		return err
	}
	if err := send(c.input, value); err != nil {
		c.cancel()
		return err
	}
	err := c.response(want, 8*time.Second)
	if err != nil {
		c.cancel()
	}
	return err
}

func monitor(ctx context.Context, leases []*lifecycle.Lock) (context.Context, func() error, error) {
	owner, err := processidentity.Lookup(os.Getpid())
	if err != nil {
		return ctx, nil, err
	}
	program, err := os.Executable()
	if err != nil {
		return ctx, nil, err
	}
	input, parentWrite, err := os.Pipe()
	if err != nil {
		return ctx, nil, err
	}
	defer input.Close()
	parentRead, output, err := os.Pipe()
	if err != nil {
		parentWrite.Close()
		return ctx, nil, err
	}
	defer output.Close()
	files := []*os.File{input, output}
	directories := []string{}
	for _, lease := range leases {
		duplicate, copyErr := lease.Inherit()
		if copyErr != nil {
			for _, file := range files[2:] {
				_ = file.Close()
			}
			parentRead.Close()
			parentWrite.Close()
			return ctx, nil, copyErr
		}
		defer duplicate.Close()
		files = append(files, duplicate)
		directories = append(directories, lease.Directory())
	}
	id := protocol.ID()
	if err = lifecycle.BeginOwnership(leases, id); err != nil {
		parentRead.Close()
		parentWrite.Close()
		return ctx, nil, err
	}
	command := exec.Command(program, "process-owner")
	command.ExtraFiles = files
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Dir = directories[0]
	command.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	command.Stderr = os.Stderr
	if err = command.Start(); err != nil {
		parentRead.Close()
		parentWrite.Close()
		return ctx, nil, errors.Join(err, lifecycle.CompleteOwnership(directories, id))
	}
	// The parent must not retain a writer that masks monitor EOF.
	_ = input.Close()
	_ = output.Close()
	guardCtx, cancel := context.WithCancel(ctx)
	c := &client{input: parentWrite, output: parentRead, replies: make(chan reply, 2), done: make(chan struct{}), wait: make(chan error, 1), cancel: cancel, program: program}
	go c.receive()
	go func() { c.wait <- command.Wait() }()
	finish := func() error {
		err := c.call(request{Operation: "finish"}, "stopped")
		_ = c.input.Close() // EOF also initiates cleanup if the final request failed.
		select {
		case waitErr := <-c.wait:
			err = errors.Join(err, waitErr)
		case <-time.After(8 * time.Second):
			err = errors.Join(err, lifecycle.ErrUnconfirmed)
		}
		_ = c.output.Close()
		cancel()
		return err
	}
	_ = parentWrite.SetWriteDeadline(time.Now().Add(8 * time.Second))
	if err = send(parentWrite, startMessage{ID: id, Owner: owner, Directories: directories}); err == nil {
		err = c.response("ready", 8*time.Second)
	}
	if err != nil {
		return ctx, nil, errors.Join(err, finish())
	}
	return childprocess.WithLaunchGuard(guardCtx, c), finish, nil
}

// Prepare temporarily replaces only the executable/argv of the caller's Cmd.
// Its stdio, environment, context cancellation, working directory and process
// group remain attached to the same child, which execs after ownership ack.
func (c *client) Prepare(ctx context.Context, command *exec.Cmd) (func() error, func(), error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if len(command.ExtraFiles) > 32 {
		return nil, nil, errors.New("too many inherited files for a guarded launch")
	}
	input, output, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	ready, readyWrite, err := os.Pipe()
	if err != nil {
		input.Close()
		output.Close()
		return nil, nil, err
	}
	path, arguments, extra := command.Path, command.Args, command.ExtraFiles
	fd := 3 + len(extra)
	command.Path = c.program
	command.Args = []string{c.program, "process-child", strconv.Itoa(fd), strconv.Itoa(fd + 1)}
	command.ExtraFiles = append(append([]*os.File{}, extra...), input, readyWrite)
	release := func() {
		command.Path, command.Args, command.ExtraFiles = path, arguments, extra
		input.Close()
		output.Close()
		ready.Close()
		readyWrite.Close()
	}
	confirm := func() error {
		_ = input.Close()
		_ = readyWrite.Close()
		deadline := time.Now().Add(6 * time.Second)
		if err := output.SetWriteDeadline(deadline); err != nil {
			return err
		}
		if err := ready.SetReadDeadline(deadline); err != nil {
			return err
		}
		if err := send(output, launchMessage{Path: path, Arguments: arguments}); err != nil {
			return err
		}
		var identity processidentity.Identity
		if err := json.NewDecoder(io.LimitReader(ready, 4096)).Decode(&identity); err != nil {
			return err
		}
		if identity.PID != command.Process.Pid {
			return errors.New("guarded child identity differs from the launched child")
		}
		if err := c.call(request{Operation: "own", Identity: identity}, "owned"); err != nil {
			return err
		}
		_, err := output.Write([]byte("execute\n"))
		return err
	}
	return confirm, release, nil
}

// Dispatch is an internal inherited-pipe protocol, not a PID-based control API.
func Dispatch(arguments []string) error {
	if len(arguments) == 1 && arguments[0] == "process-owner" {
		return watch()
	}
	if len(arguments) != 3 || arguments[0] != "process-child" {
		return errors.New("invalid internal process mode")
	}
	fd, err := strconv.Atoi(arguments[1])
	readyFD, readyErr := strconv.Atoi(arguments[2])
	if err != nil || readyErr != nil || fd < 3 || fd > 35 || readyFD != fd+1 {
		return errors.New("invalid guarded launch descriptors")
	}
	input, output := os.NewFile(uintptr(fd), "guarded-launch"), os.NewFile(uintptr(readyFD), "guarded-ready")
	defer input.Close()
	defer output.Close()
	syscall.CloseOnExec(fd)
	syscall.CloseOnExec(readyFD)
	_ = input.SetReadDeadline(time.Now().Add(10 * time.Second))
	_ = output.SetWriteDeadline(time.Now().Add(10 * time.Second))
	read := scanner(input)
	var message launchMessage
	if !read.Scan() || json.Unmarshal(read.Bytes(), &message) != nil || message.Path == "" || len(message.Arguments) == 0 {
		return errors.New("invalid guarded launch payload")
	}
	identity, err := processidentity.Lookup(os.Getpid())
	if err != nil {
		return err
	}
	if err = send(output, identity); err != nil {
		return err
	}
	if !read.Scan() || read.Text() != "execute" {
		return errors.New("guarded launch was not authorized by its monitor")
	}
	_ = input.Close()
	_ = output.Close()
	return syscall.Exec(message.Path, message.Arguments, os.Environ())
}
