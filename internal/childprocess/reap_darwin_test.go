//go:build darwin

package childprocess

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestDarwinExitedGroupIsConfirmedOnlyAfterOwnChildIsReaped(t *testing.T) {
	for _, reap := range []bool{true, false} {
		t.Run(map[bool]string{true: "confirmed-reap", false: "unconfirmed-reap"}[reap], func(t *testing.T) {
			cmd := exec.Command("/bin/sh", "-c", "exit 0")
			tree, err := prepare(cmd)
			if err != nil {
				t.Fatal(err)
			}
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			cmd.Stdout = writer
			if err = cmd.Start(); err != nil {
				writer.Close()
				t.Fatal(err)
			}
			writer.Close()
			if err = tree.started(cmd); err != nil {
				t.Fatal(err)
			}
			if _, err = io.Copy(io.Discard, reader); err != nil {
				t.Fatal(err)
			}
			// This owned zombie has not been waited on. Confirm the actual XNU
			// condition before exercising the library's concurrent reaper path.
			if err = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); !errors.Is(err, syscall.EPERM) {
				cmd.Wait()
				t.Fatalf("expected native unreaped-group EPERM, got %v", err)
			}
			p := &Process{tree: tree, reaped: make(chan struct{})}
			if reap {
				go func() { time.Sleep(10 * time.Millisecond); _ = cmd.Wait(); close(p.reaped) }()
			} else {
				defer cmd.Wait()
			}
			err = p.Kill()
			if reap && err != nil {
				t.Fatal("reaped, empty owned group remained an error", err)
			}
			if !reap && !errors.Is(err, syscall.EPERM) {
				t.Fatal("unconfirmed signal failure was hidden", err)
			}
		})
	}
}
