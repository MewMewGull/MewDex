//go:build darwin || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type fixtureLifetime struct {
	Until        time.Time `json:"until"`
	NativeEngine bool      `json:"nativeEngine,omitempty"`
	Generation   string    `json:"generation,omitempty"`
}

type nativeExecution struct {
	EnginePID     int    `json:"enginePid"`
	EngineGroup   int    `json:"engineGroup"`
	EngineProgram string `json:"engineProgram"`
	Version       string `json:"version"`
	SessionID     string `json:"sessionId"`
	ThreadID      string `json:"threadId"`
	TurnID        string `json:"turnId"`
	SessionState  string `json:"sessionState"`
	StopStatus    string `json:"stopStatus"`
}

type runtimeProcess struct {
	PID     int    `json:"pid"`
	Group   int    `json:"group"`
	Program string `json:"program"`
}

func ownedRuntimes(ctx context.Context, root string) ([]runtimeProcess, error) {
	raw, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,pgid=,stat=,comm=").Output()
	if err != nil {
		return nil, err
	}
	values := []runtimeProcess{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || strings.HasPrefix(fields[2], "Z") {
			continue
		}
		program := strings.Join(fields[3:], " ")
		if !strings.HasPrefix(program, root+string(filepath.Separator)) {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		group, groupErr := strconv.Atoi(fields[1])
		if pidErr != nil || groupErr != nil || pid <= 1 {
			return nil, errors.New("invalid owned runtime identity")
		}
		values = append(values, runtimeProcess{PID: pid, Group: group, Program: program})
	}
	return values, nil
}

// Probe cleanup only: freshly re-observe exact private runtime paths and group
// identities before ending any leftovers. Never signal a process by name.
func cleanOwnedRuntimes(ctx context.Context, root string) error {
	values, err := ownedRuntimes(ctx, root)
	if err != nil {
		return err
	}
	for _, value := range values {
		fresh, err := ownedRuntimes(ctx, root)
		if err != nil {
			return err
		}
		for _, current := range fresh {
			if current == value {
				if err = syscall.Kill(current.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
					return err
				}
			}
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		values, err = ownedRuntimes(ctx, root)
		if err != nil || len(values) == 0 {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("private native runtime cleanup is unconfirmed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Include only this trusted probe executable in the normal execution scope.
// All remaining permissions and command handling come from the real executor.
type fixtureEngine struct {
	*engine.Codex
	program string
}

func (e fixtureEngine) ExecutionEnvironment() (engine.ExecutionEnvironment, error) {
	value, err := e.Codex.ExecutionEnvironment()
	value.RuntimeReadOnly = append(value.RuntimeReadOnly, e.program)
	return value, err
}

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// Observe a direct child by parent and its exact verified executable path.
// This does not search for a similarly named installed engine or user process.
func directEngine(ctx context.Context, program string) (int, error) {
	raw, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return 0, err
	}
	pid := 0
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		parent, _ := strconv.Atoi(fields[1])
		if parent != os.Getpid() || strings.Join(fields[2:], " ") != program {
			continue
		}
		value, parseErr := strconv.Atoi(fields[0])
		if parseErr != nil || pid != 0 {
			return 0, errors.New("engine child identity is ambiguous")
		}
		pid = value
	}
	if pid <= 1 {
		return 0, errors.New("the verified native engine child was not observed")
	}
	return pid, nil
}

func startNativeFixture(ctx context.Context, c config.Config, root, program string) (nativeExecution, func(), error) {
	var value nativeExecution
	var real *engine.Codex
	var state *store.Store
	var controller *executor.Executor
	closeAll := func() {
		if controller != nil {
			_ = controller.Close()
		}
		if real != nil {
			_ = real.Close()
		}
		if state != nil {
			_ = state.Close()
		}
	}
	// Do not inherit credentials or a proxy route into this loopback-only probe.
	for _, name := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		_ = os.Unsetenv(name)
	}
	_ = os.Setenv("NO_PROXY", "127.0.0.1,localhost")
	c.Engine.Args = []string{"--strict-config", "-c", fmt.Sprintf(`projects={%q={trust_level="trusted"}}`, c.Projects[0].Path)}
	diagnostics, err := os.OpenFile(filepath.Join(filepath.Dir(root), "native-engine.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return value, closeAll, err
	}
	closeResources := closeAll
	closeAll = func() { closeResources(); _ = diagnostics.Close() }
	real, err = engine.StartWithOptions(ctx, c.Engine, engine.StartOptions{Diagnostics: diagnostics})
	if err != nil {
		return value, closeAll, err
	}
	value.EnginePID, err = directEngine(ctx, c.Engine.Executable)
	if err != nil {
		return value, closeAll, err
	}
	value.EngineGroup, err = syscall.Getpgid(value.EnginePID)
	if err != nil {
		return value, closeAll, err
	}
	value.EngineProgram, value.Version = c.Engine.Executable, engine.CompatibleVersion
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if err = real.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		return value, closeAll, err
	}
	if string(account.Account) != "null" {
		return value, closeAll, errors.New("isolated engine unexpectedly has an account")
	}
	state, err = store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		return value, closeAll, err
	}
	controller, err = executor.New(c, state, fixtureEngine{Codex: real, program: program}, nil)
	if err != nil {
		return value, closeAll, err
	}
	created := controller.Handle(ctx, protocol.Command{ID: "managed-exit-create", SourceID: "owned-fixture", DeviceID: c.DeviceID, ProjectID: c.Projects[0].ID, Kind: "create"})
	if created.State != "completed" || created.Session == nil {
		return value, closeAll, fmt.Errorf("create: %s: %s", created.State, created.Error)
	}
	started := controller.Handle(ctx, protocol.Command{ID: "managed-exit-start", SourceID: "owned-fixture", DeviceID: c.DeviceID, SessionID: created.Session.ID, Kind: "start", Input: []protocol.Input{{Type: "text", Text: "Run the deterministic local process fixture; leave its short background workload running."}}})
	if started.State != "completed" || started.Session == nil {
		return value, closeAll, fmt.Errorf("start: %s: %s", started.State, started.Error)
	}
	for {
		var session protocol.Session
		if err = state.Get("sessions", created.Session.ID, &session); err != nil {
			return value, closeAll, err
		}
		if session.StopStatus == "background-running" {
			value.SessionID, value.ThreadID, value.TurnID = session.ID, session.EngineID, session.TurnID
			value.SessionState, value.StopStatus = session.State, session.StopStatus
			return value, closeAll, nil
		}
		interactions, listErr := store.List[protocol.Interaction](state, "interactions")
		if listErr != nil {
			return value, closeAll, listErr
		}
		for _, interaction := range interactions {
			if interaction.State == "pending" {
				return value, closeAll, errors.New("unexpected approval in fixed process fixture")
			}
		}
		select {
		case <-ctx.Done():
			return value, closeAll, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
