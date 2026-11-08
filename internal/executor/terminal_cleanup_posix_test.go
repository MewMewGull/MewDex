//go:build darwin || linux

package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestUnconfirmedNativeStopRemainsUnknownAndBlocksOverlappingWork(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(root, "sandbox-fixture.sh")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  --version) printf 'mewdex-sandbox %s\n'; exit 0 ;;
  --probe) printf '{"protocolVersion":1,"available":true}\n'; exit 0 ;;
esac
cat >/dev/null
trap 'kill -KILL $$' TERM
printf READY
while :; do sleep 1; done
`, engine.NativeSandboxVersion)
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	x, err := NewTerminal(ctx, config.Config{DeviceID: "native-cleanup", DataDir: filepath.Join(root, "data"), Projects: []protocol.Project{{ID: "project", Path: project}}, Engine: config.Engine{Executable: program}}, database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })
	command := protocol.Command{ID: "cleanup-job", ProjectID: "project", Kind: "terminal.exec"}
	command.Terminal = &protocol.TerminalRequest{OwnerID: "owner", Command: []string{"fixture"}, YieldMS: 1000}
	result := x.terminalCommand(ctx, command)
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	var job protocol.TerminalJob
	deadline := time.Now().Add(4 * time.Second)
	for {
		if err = database.Get("terminal_jobs", command.ID, &job); err != nil {
			t.Fatal(err)
		}
		if job.Stdout == "READY" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sandbox fixture did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopped := x.terminalCommand(ctx, protocol.Command{Kind: "terminal.stop", ProjectID: "project", Terminal: &protocol.TerminalRequest{OwnerID: "owner", JobID: command.ID, YieldMS: 5000}})
	if json.Unmarshal(stopped.Data, &job) != nil || job.State != "unknown" {
		t.Fatal("unconfirmed sandbox cleanup was reported as stopped", string(stopped.Data))
	}
	for _, owner := range []string{"owner", "another-owner"} {
		command.ID, command.Terminal.OwnerID = "next-"+owner, owner
		if result = x.terminalCommand(ctx, command); result.State != "failed" {
			t.Fatal("unconfirmed execution released the overlapping workspace", owner, result)
		}
	}
	result = x.terminalCommand(ctx, protocol.Command{Kind: "terminal.stopOwner", Terminal: &protocol.TerminalRequest{OwnerID: "owner"}})
	if result.State != "failed" {
		t.Fatal("owner shutdown acknowledged an unconfirmed sandbox cleanup", result)
	}
}
