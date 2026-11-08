//go:build darwin || linux

package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/config"
	"mewdex/internal/executionscope"
)

func TestNativeSandboxStopsDetachedDescendants(t *testing.T) {
	program := os.Getenv("MEWDEX_TEST_SANDBOX")
	if program == "" {
		t.Skip("set MEWDEX_TEST_SANDBOX to run real native POSIX cancellation checks")
	}
	for _, mode := range []string{"context", "explicit", "normal-exit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			root := t.TempDir()
			sandbox, err := NewSandbox(ctx, config.Engine{Executable: program, Home: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			environment, err := sandbox.Environment()
			if err != nil {
				t.Fatal(err)
			}
			profile, err := executionscope.Build(executionscope.Spec{DeviceID: "native", ProjectID: "detached-child", EngineVersion: sandbox.Version(), Workspace: root, Private: environment.Private, ReadOnly: environment.RuntimeReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			ready, late := filepath.Join(root, "ready"), filepath.Join(root, "late")
			// The descendant leaves its inherited process group before signalling
			// readiness. A process-group-only shutdown cannot remove it.
			script := `use POSIX qw(setsid); my $child = fork(); die "fork" unless defined $child; if ($child == 0) { setsid() >= 0 or die "setsid"; close STDOUT; close STDERR; open my $ready, ">", $ARGV[0] or die "ready"; print $ready "READY"; close $ready; sleep 3; open my $late, ">", $ARGV[1] or die "late"; print $late "LATE"; close $late; exit 0; } `
			if mode == "normal-exit" {
				script += `while (!-e $ARGV[0]) { select undef, undef, undef, 0.01; } exit 0;`
			} else {
				script += `sleep 12;`
			}
			command, err := sandbox.Command(ctx, profile, root, []string{"/usr/bin/perl", "-e", script, ready, late})
			if err != nil {
				t.Fatal(err)
			}
			process, err := childprocess.StartContext(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cancel(); _ = process.Kill(); _ = process.Wait() })
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err = os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("detached sandbox descendant did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			started := time.Now()
			if mode == "context" {
				cancel()
			}
			if mode == "explicit" {
				if err = process.Kill(); err != nil {
					t.Fatal(err)
				}
			}
			err = process.Wait()
			if (mode == "normal-exit" && err != nil) || (mode != "normal-exit" && err == nil) || time.Since(started) > 5*time.Second {
				t.Fatal("unexpected sandbox shutdown result", time.Since(started), err)
			}
			time.Sleep(3500 * time.Millisecond)
			if _, err = os.Stat(late); !os.IsNotExist(err) {
				t.Fatal("detached descendant survived sandbox shutdown", err)
			}
		})
	}
}

func TestNativeSandboxDistinguishesPayloadFailureFromUnconfirmedHostExit(t *testing.T) {
	for _, test := range []struct {
		mode        string
		unconfirmed bool
	}{{"command-failed", false}, {"unconfirmed", true}} {
		t.Run(test.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			sandbox, err := NewSandbox(ctx, nativeSandboxConfig(t, test.mode))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			profile, err := executionscope.Build(executionscope.Spec{DeviceID: "native", ProjectID: "exit-result", EngineVersion: sandbox.Version(), Workspace: root})
			if err != nil {
				t.Fatal(err)
			}
			command, err := sandbox.Command(ctx, profile, root, []string{"fixture"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = childprocess.OutputContext(ctx, command, 1024); err == nil {
				t.Fatal("fixture did not fail")
			}
			if sandbox.UnconfirmedExit(command) != test.unconfirmed {
				t.Fatal("payload failure and unconfirmed cleanup were conflated")
			}
		})
	}
}
