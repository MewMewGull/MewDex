package childprocess

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mewdex/internal/lifecycle"
)

func fixtureCommand(root, mode string) *exec.Cmd {
	return exec.Command(os.Args[0], "-test.run=^TestOwnedProcessFixture$", "--", mode, root)
}

func TestOwnedProcessFixture(t *testing.T) {
	if os.Getenv("MEWDEX_TEST_OWNED_PROCESS") != "1" {
		return
	}
	// A failed test cannot leave its fixture running indefinitely.
	time.AfterFunc(15*time.Second, func() { os.Exit(3) })
	arguments := os.Args[len(os.Args)-2:]
	mode, root := arguments[0], arguments[1]
	lease, err := lifecycle.Acquire(filepath.Join(root, mode+"-lease"))
	if err != nil {
		os.Exit(4)
	}
	defer lease.Close()
	if mode == "owner" {
		command := fixtureCommand(root, "parent-ignore")
		if _, err = Start(command); err != nil {
			os.Exit(5)
		}
	}
	if strings.HasPrefix(mode, "parent") || mode == "child" {
		next := "child"
		if mode == "child" {
			next = "grandchild"
		}
		command := fixtureCommand(root, next)
		// Descendants deliberately keep the parent's output descriptors alive.
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err = command.Start(); err != nil {
			os.Exit(6)
		}
	}
	if err = os.WriteFile(filepath.Join(root, mode+".ready"), []byte("ready"), 0600); err != nil {
		os.Exit(7)
	}
	switch mode {
	case "cooperative", "cooperative-slow":
		for {
			if _, err := os.Stat(filepath.Join(root, "shutdown.request")); err == nil {
				if mode == "cooperative-slow" {
					time.Sleep(2 * time.Second)
				}
				_ = os.WriteFile(filepath.Join(root, "shutdown.completed"), []byte("completed"), 0600)
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	case "parent", "unrelated":
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "output":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", 100000))
	default:
		time.Sleep(14 * time.Second)
	}
	os.Exit(0)
}

func TestOwnedProcessKillUsesShutdownHook(t *testing.T) {
	t.Setenv("MEWDEX_TEST_OWNED_PROCESS", "1")
	for _, mode := range []string{"cooperative", "cooperative-slow"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			command := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestOwnedProcessFixture$", "--", mode, root)
			command.WaitDelay = 3 * time.Second
			command.Cancel = func() error {
				return os.WriteFile(filepath.Join(root, "shutdown.request"), []byte("stop"), 0600)
			}
			process, err := Start(command)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = process.Kill(); _ = process.Wait() })
			waitReady(t, root, mode)
			if err = process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err = process.Wait(); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(root, "shutdown.completed")); err != nil {
				t.Fatal("owned process was killed before its shutdown hook completed", err)
			}
		})
	}
}

func waitReady(t *testing.T, root string, modes ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, mode := range modes {
		for {
			if _, err := os.Stat(filepath.Join(root, mode+".ready")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("owned process fixture did not become ready: %s", mode)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func requireReleased(t *testing.T, root string, modes ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, mode := range modes {
		for {
			lease, err := lifecycle.Acquire(filepath.Join(root, mode+"-lease"))
			if err == nil {
				lease.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("owned descendant still holds its OS lease: %s", mode)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestMainExitClearsDescendantsHoldingOutputOpen(t *testing.T) {
	t.Setenv("MEWDEX_TEST_OWNED_PROCESS", "1")
	root := t.TempDir()
	command := fixtureCommand(root, "parent")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	command.Stdout = writer
	process, err := Start(command)
	writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer process.Kill()
	waitReady(t, root, "parent", "child", "grandchild")
	input.Close()
	select {
	case <-process.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("main process wait was blocked by descendant output handles")
	}
	if err = process.Wait(); err != nil {
		t.Fatal(err)
	}
	requireReleased(t, root, "child", "grandchild")
	eof := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); eof <- err }()
	select {
	case err = <-eof:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("descendant output handle remained open after cleanup")
	}
}

func TestKillOwnsDescendantsAndPreservesUnrelatedProcess(t *testing.T) {
	t.Setenv("MEWDEX_TEST_OWNED_PROCESS", "1")
	root := t.TempDir()
	unrelated := fixtureCommand(root, "unrelated")
	unrelatedInput, err := unrelated.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { unrelatedInput.Close(); _ = unrelated.Wait() }()
	process, err := Start(fixtureCommand(root, "parent-ignore"))
	if err != nil {
		t.Fatal(err)
	}
	defer process.Kill()
	waitReady(t, root, "unrelated", "child", "grandchild")
	if err = process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("owned main process did not exit after termination")
	}
	requireReleased(t, root, "parent-ignore", "child", "grandchild")
	if lease, err := lifecycle.Acquire(filepath.Join(root, "unrelated-lease")); err == nil {
		lease.Close()
		t.Fatal("cleanup affected a process outside its owned tree")
	}
	if err = process.Kill(); err != nil {
		t.Fatal("repeated cleanup did not preserve its original outcome")
	}
}

func TestOutputCancellationClearsDescendants(t *testing.T) {
	t.Setenv("MEWDEX_TEST_OWNED_PROCESS", "1")
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnedProcessFixture$", "--", "parent-ignore", root)
	if _, err := Output(command, 1000); err == nil {
		t.Fatal("cancelled child capture reported success")
	}
	waitReady(t, root, "child", "grandchild")
	requireReleased(t, root, "parent-ignore", "child", "grandchild")
}

func TestOutputLimitTerminatesItsProducer(t *testing.T) {
	t.Setenv("MEWDEX_TEST_OWNED_PROCESS", "1")
	root := t.TempDir()
	if _, err := Output(fixtureCommand(root, "output"), 1000); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatal("unbounded producer output was retained")
	}
	requireReleased(t, root, "output")
}

func TestWindowsOwnerExitClosesItsJob(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("kill-on-close job ownership is Windows-specific")
	}
	t.Setenv("MEWDEX_TEST_OWNED_PROCESS", "1")
	root := t.TempDir()
	owner := fixtureCommand(root, "owner")
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	waitReady(t, root, "owner", "parent-ignore", "child", "grandchild")
	if err := owner.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatal(err)
	}
	requireReleased(t, root, "parent-ignore", "child", "grandchild")
}
