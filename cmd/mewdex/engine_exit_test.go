package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
)

func TestUnexpectedEngineExitStopsTheService(t *testing.T) {
	t.Setenv("MEWDEX_TEST_SERVICE_ENGINE", "1")
	root := t.TempDir()
	trigger := filepath.Join(root, "exit-engine")
	t.Setenv("MEWDEX_TEST_SERVICE_ENGINE_EXIT", trigger)
	c := config.Config{DeviceID: "engine-exit-fixture", Name: "isolated fixture", Hub: true, Executor: true, Listen: "127.0.0.1:0", DataDir: filepath.Join(root, "data"), Projects: []protocol.Project{{ID: "fixture", Path: root}}, Engine: config.Engine{Executable: os.Args[0], Args: []string{"-test.run=^TestServiceEngineFixture$", "--"}, Home: filepath.Join(root, "engine-home")}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, c) }()
	ready := false
	for !ready {
		if record, err := lifecycle.Read(c.DataDir); err == nil {
			state, callErr := lifecycle.Call(ctx, record, false)
			ready = callErr == nil && state["ready"] == true
		}
		if ready {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("service exited before becoming ready: %v", err)
		case <-ctx.Done():
			t.Fatal("service was not ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := os.WriteFile(trigger, []byte("exit"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "engine exited") {
			t.Fatalf("unhealthy service did not report its engine failure: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("service stayed running after its engine disconnected")
	}
	lease, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		t.Fatal("failed service retained its data lease")
	}
	lease.Close()
}

func TestServiceEngineFixture(t *testing.T) {
	if os.Getenv("MEWDEX_TEST_SERVICE_ENGINE") != "1" {
		return
	}
	for _, arg := range os.Args {
		if arg == "--version" {
			fmt.Println("codex-cli " + engine.CompatibleVersion)
			os.Exit(0)
		}
	}
	go func() {
		for {
			if _, err := os.Stat(os.Getenv("MEWDEX_TEST_SERVICE_ENGINE_EXIT")); err == nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) == nil && len(request.ID) > 0 {
			fmt.Printf("{\"id\":%s,\"result\":{}}\n", request.ID)
		}
	}
	os.Exit(0)
}
