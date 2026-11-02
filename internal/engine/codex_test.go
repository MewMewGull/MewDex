package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"mewdex/internal/config"
)

func TestRPCResponseIsIndependentOfNotificationConsumer(t *testing.T) {
	t.Setenv("MEWDEX_TEST_RPC", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e, err := Start(ctx, config.Engine{Executable: os.Args[0], Args: []string{"-test.run=TestRPCFixtureProcess", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var result struct {
		OK bool `json:"ok"`
	}
	// No notification consumer runs while the fixture emits more than the old
	// channel capacity before replying to the command.
	if err = e.Call(ctx, "fixture/flood", map[string]any{}, &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatal("missing command response")
	}
	_ = e.Close()
	count := 0
	for range e.Notifications() {
		count++
	}
	if count != 1800 {
		t.Fatalf("lost ordered notifications: %d", count)
	}
}

func TestRPCFixtureProcess(t *testing.T) {
	if os.Getenv("MEWDEX_TEST_RPC") != "1" {
		return
	}
	for _, arg := range os.Args {
		if arg == "--version" {
			fmt.Println("codex-cli " + CompatibleVersion)
			os.Exit(0)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	for scanner.Scan() {
		var request rpcMessage
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		if request.Method == "fixture/flood" {
			for i := 0; i < 1800; i++ {
				fmt.Fprintln(writer, `{"method":"item/agentMessage/delta","params":{"threadId":"thread","delta":"x"}}`)
			}
		}
		if request.Method == "thread/read" {
			value, _ := json.Marshal(map[string]any{"id": request.ID, "result": map[string]any{"thread": map[string]string{"id": "owned", "path": os.Getenv("MEWDEX_TEST_ROLLOUT")}}})
			fmt.Fprintln(writer, string(value))
			_ = writer.Flush()
			continue
		}
		if request.Method == "fixture/environment" {
			_, bridge := os.LookupEnv("CODEX_APP_TOOLS_PIPE_PATH")
			_, thread := os.LookupEnv("CODEX_THREAD_ID")
			_, permission := os.LookupEnv("CODEX_PERMISSION_PROFILE")
			value, _ := json.Marshal(map[string]any{"id": request.ID, "result": map[string]any{"bridge": bridge, "thread": thread, "permission": permission, "home": os.Getenv("CODEX_HOME")}})
			fmt.Fprintln(writer, string(value))
			_ = writer.Flush()
			continue
		}
		if request.Method == "fixture/invalid-shape" {
			fmt.Fprintf(writer, "{\"id\":%s,\"result\":{\"turn\":\"invalid\"}}\n", request.ID)
			_ = writer.Flush()
			continue
		}
		fmt.Fprintf(writer, "{\"id\":%s,\"result\":{\"ok\":true}}\n", request.ID)
		_ = writer.Flush()
	}
	os.Exit(0)
}

func TestIndependentEngineDoesNotInheritHostToolChannels(t *testing.T) {
	t.Setenv("MEWDEX_TEST_RPC", "1")
	t.Setenv("CODEX_APP_TOOLS_PIPE_PATH", "synthetic-host-pipe")
	t.Setenv("CODEX_THREAD_ID", "synthetic-host-thread")
	t.Setenv("CODEX_PERMISSION_PROFILE", "synthetic-host-permission")
	t.Setenv("CODEX_HOME", t.TempDir())
	owned := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e, err := Start(ctx, config.Engine{Executable: os.Args[0], Args: []string{"-test.run=TestRPCFixtureProcess", "--"}, Home: owned})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var response struct {
		Bridge, Thread, Permission bool
		Home                       string
	}
	if err = e.Call(ctx, "fixture/environment", nil, &response); err != nil {
		t.Fatal(err)
	}
	if response.Bridge || response.Thread || response.Permission || response.Home != owned {
		t.Fatal("independent engine inherited host controls", response)
	}
}

func TestUnusableEngineReceiptKeepsUnknownOutcome(t *testing.T) {
	t.Setenv("MEWDEX_TEST_RPC", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e, err := Start(ctx, config.Engine{Executable: os.Args[0], Args: []string{"-test.run=TestRPCFixtureProcess", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var reply struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err = e.Call(ctx, "fixture/invalid-shape", map[string]any{}, &reply); !IsUnknownOutcome(err) {
		t.Fatalf("unusable receipt became a definite failure: %v", err)
	}
}

func TestBaseInstructionsOnlyReadTheOwnedEngineDirectory(t *testing.T) {
	for _, test := range []struct{ outside, extended bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprintf("outside=%v/extended=%v", test.outside, test.extended), func(t *testing.T) {
			if test.extended && runtime.GOOS != "windows" {
				t.Skip("Windows extended paths")
			}
			outside := test.outside
			root := t.TempDir()
			home := filepath.Join(root, "engine-home")
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, "rollout.jsonl")
			if outside {
				path = filepath.Join(root, "foreign.jsonl")
			}
			if err := os.WriteFile(path, []byte(`{"type":"session_meta","payload":{"id":"owned","base_instructions":{"text":"instruction snapshot"}}}`+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MEWDEX_TEST_RPC", "1")
			rpcPath := path
			if test.extended {
				rpcPath = `\\?\` + path
			}
			t.Setenv("MEWDEX_TEST_ROLLOUT", rpcPath)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			e, err := Start(ctx, config.Engine{Executable: os.Args[0], Args: []string{"-test.run=TestRPCFixtureProcess", "--"}, Home: home})
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			text, err := e.ReadBaseInstructions(ctx, "owned")
			if outside {
				if err == nil || text != "" {
					t.Fatal("read outside engine data boundary")
				}
			} else if err != nil || text != "instruction snapshot" {
				t.Fatalf("owned instructions: %q %v", text, err)
			}
		})
	}
}

func TestAlreadyCancelledRPCDoesNotReachTheWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// No writer or pending map exists: cancellation must return before either is used.
	e := &Codex{}
	err := e.Call(ctx, "turn/start", map[string]any{}, nil)
	if !errors.Is(err, context.Canceled) || IsUnknownOutcome(err) {
		t.Fatalf("pre-send cancellation: %v", err)
	}
}
