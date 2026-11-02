//go:build darwin

package processidentity

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestIdentityFixtureProcess(t *testing.T) {
	if os.Getenv("MEWDEX_PROCESS_IDENTITY_TEST") != "1" {
		return
	}
	time.AfterFunc(15*time.Second, func() { os.Exit(3) })
	fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestKernelSignalRejectsStaleExecGeneration(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestIdentityFixtureProcess$")
	command.Env = append(os.Environ(), "MEWDEX_PROCESS_IDENTITY_TEST=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		_ = input.Close()
		if !waited {
			_ = command.Wait()
		}
	}()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("owned native fixture readiness: %q, %v", line, err)
	}
	owned, err := Lookup(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	signalAPI.once.Do(loadSignal)
	if signalAPI.err != nil {
		t.Fatal(signalAPI.err)
	}
	var stale [8]uint32
	stale[5], stale[7] = uint32(owned.PID), owned.PIDVersion+1
	result := signalAPI.call(unsafe.Pointer(&stale), int32(syscall.SIGKILL))
	runtime.KeepAlive(stale)
	if syscall.Errno(result) != syscall.ESRCH {
		t.Fatalf("kernel accepted or ambiguously rejected the stale generation: %d", result)
	}
	current, err := Lookup(owned.PID)
	if err != nil || !owned.SameLifetime(current) {
		t.Fatalf("stale signal affected its owned negative control: %v", err)
	}
	if err = Signal(current, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	waited = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("valid generation was not ended: %v", err)
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || status.Signal() != syscall.SIGKILL {
		t.Fatalf("unexpected owned exit status: %v", exit.Sys())
	}
}
