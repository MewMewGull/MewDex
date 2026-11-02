// Exercise the real executor and pinned native engine in an unsigned home.
// Model starts are captured and refused before reaching the engine.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"mewdex/internal/attachments"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type guardedEngine struct {
	engine.Engine
	mu        sync.Mutex
	turns     []json.RawMessage
	threads   []string
	extraRead string
}

func (g *guardedEngine) Call(ctx context.Context, method string, params, out any) error {
	g.mu.Lock()
	if method == "turn/start" {
		g.turns = append(g.turns, protocol.JSON(params))
		g.mu.Unlock()
		return errors.New("scope probe captured this model start; it was not forwarded")
	}
	if method == "thread/start" || method == "thread/resume" || method == "thread/fork" {
		g.threads = append(g.threads, method)
	}
	g.mu.Unlock()
	return g.Engine.Call(ctx, method, params, out)
}

func (g *guardedEngine) ExecutionEnvironment() (engine.ExecutionEnvironment, error) {
	environment, err := g.Engine.ExecutionEnvironment()
	g.mu.Lock()
	extra := g.extraRead
	g.mu.Unlock()
	if extra != "" {
		environment.RuntimeReadOnly = append(environment.RuntimeReadOnly, extra)
	}
	return environment, err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (resultErr error) {
	path := flag.String("config", ".local/device.json", "existing profile supplying only a native pinned program")
	flag.Parse()
	if runtime.GOOS == "windows" {
		return errors.New("this unsigned native scope check is for Unix; Windows setup is never implicit")
	}
	source, err := config.Load(*path)
	if err != nil {
		return err
	}
	program, arguments, err := engine.Resolve(source.Engine)
	if err != nil {
		return err
	}
	if len(arguments) != 0 {
		return errors.New("this probe needs the pinned native program without launcher or provider overrides")
	}
	root, err := os.MkdirTemp(".state", "executor-scope-risk-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	report := map[string]any{"root": root, "at": protocol.Now(), "platform": runtime.GOOS, "engineVersion": engine.CompatibleVersion, "unsignedHome": true, "modelTurnsForwarded": 0, "liveServiceChanged": false, "systemSetupRequested": false, "finalAcceptance": false}
	defer func() {
		report["state"] = "succeeded"
		if resultErr != nil {
			report["state"], report["error"] = "failed", resultErr.Error()
		}
		if err := os.WriteFile(filepath.Join(root, "inspection.json"), protocol.JSON(report), 0600); err != nil {
			resultErr = err
		}
		fmt.Println(string(protocol.JSON(report)))
	}()
	workspace := filepath.Join(root, "workspace")
	local := filepath.Join(workspace, ".local")
	data, home := filepath.Join(local, "data"), filepath.Join(local, "engine-home")
	for _, directory := range []string{data, home} {
		if err = os.MkdirAll(directory, 0700); err != nil {
			return err
		}
	}
	c := config.Config{DeviceID: "scope-risk-" + protocol.ID(), Name: "MewDex unsigned execution scope risk", DataDir: data, Hub: true, Executor: true, Projects: []protocol.Project{{ID: "risk", Path: workspace}}, Engine: config.Engine{Executable: program, Args: []string{"--strict-config"}, Home: home}}
	c.ProfilePath = filepath.Join(local, "device.json")
	if err = os.WriteFile(c.ProfilePath, protocol.JSON(c), 0600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(local, "client-auth-fixture.json"), []byte("private fixture, no credentials"), 0600); err != nil {
		return err
	}
	assets, err := attachments.New(filepath.Join(data, "attachments"))
	if err != nil {
		return err
	}
	meta, err := assets.SaveTarget("scoped-input.txt", strings.NewReader("scope probe input, no model"), "probe-input", c.DeviceID, "risk")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	var session protocol.Session
	var priorScope string
	for phase := 0; phase < 2; phase++ {
		if err = runPhase(ctx, c, meta, &session, phase, report); err != nil {
			return err
		}
		if phase == 0 {
			priorScope = session.ExecutionScopeID
		} else if session.ExecutionScopeID != priorScope {
			return errors.New("a service restart changed the execution scope without a path or config change")
		}
	}
	report["scopeId"] = priorScope
	report["sessionId"] = session.ID
	report["restartPreservedScope"] = true
	return nil
}

func runPhase(ctx context.Context, c config.Config, meta protocol.Attachment, session *protocol.Session, phase int, report map[string]any) (resultErr error) {
	real, err := engine.Start(ctx, c.Engine)
	if err != nil {
		return err
	}
	defer real.Close()
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if err = real.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		return err
	}
	if string(account.Account) != "null" {
		return errors.New("isolated engine unexpectedly has model authorization")
	}
	state, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		return err
	}
	defer state.Close()
	if phase > 0 {
		// Only this probe's definitely unforwarded input is reset. No live or
		// unknown task is adopted, retried, or replayed.
		if session.EngineStarted || session.LastStartID != "" {
			return errors.New("probe no longer describes an unsubmitted model start")
		}
		session.State, session.Frozen = "idle", false
		if err = state.Put("sessions", session.ID, session); err != nil {
			return err
		}
	}
	guard := &guardedEngine{Engine: real}
	x, err := executor.New(c, state, guard, nil)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := x.Close()
		report[fmt.Sprintf("phase%dEngineClosed", phase)] = closeErr == nil
		if closeErr != nil && resultErr == nil {
			resultErr = closeErr
		}
	}()
	if phase == 0 {
		created := x.Handle(ctx, protocol.Command{ID: "scope-create", SourceID: "probe-input", DeviceID: c.DeviceID, ProjectID: "risk", Kind: "create"})
		if created.State != "completed" || created.Session == nil || created.Session.ExecutionScopeID == "" {
			return fmt.Errorf("scoped create: %s: %s", created.State, created.Error)
		}
		*session = *created.Session
	}
	result := x.Handle(ctx, protocol.Command{ID: fmt.Sprintf("scope-start-%d", phase), SourceID: "probe-input", DeviceID: c.DeviceID, SessionID: session.ID, Kind: "start", Input: []protocol.Input{{Type: "file", AttachmentID: meta.ID}}})
	if result.State != "failed" || !strings.Contains(result.Error, "was not forwarded") {
		return fmt.Errorf("model-start guard was not reached safely: %s: %s", result.State, result.Error)
	}
	guard.mu.Lock()
	turns := append([]json.RawMessage(nil), guard.turns...)
	threads := append([]string(nil), guard.threads...)
	guard.mu.Unlock()
	if len(turns) != 1 || len(threads) != 1 || threads[0] != "thread/start" {
		return errors.New("expected one scoped empty thread and one captured, unforwarded start per phase")
	}
	var captured struct {
		Permissions string `json:"permissions"`
		Cwd         string `json:"cwd"`
		Input       []struct {
			Text string `json:"text"`
		} `json:"input"`
	}
	if json.Unmarshal(turns[0], &captured) != nil || captured.Permissions != "" || captured.Cwd != c.Projects[0].Path || len(captured.Input) != 1 {
		return errors.New("captured model start lost its bound scope or attachment")
	}
	if strings.Contains(captured.Input[0].Text, c.DataDir) || !strings.Contains(captured.Input[0].Text, "mewdex-inputs-") {
		return errors.New("model input still exposes a private cached attachment path")
	}
	if err = state.Get("sessions", session.ID, session); err != nil {
		return err
	}
	if session.EngineStarted || session.LastStartID != "" {
		return errors.New("a refused, unforwarded model start became submitted state")
	}
	changedRejected := false
	if phase == 1 {
		extra := filepath.Join(filepath.Dir(c.Projects[0].Path), "new-public-runtime.txt")
		if err = os.WriteFile(extra, []byte("controlled environment change"), 0600); err != nil {
			return err
		}
		guard.mu.Lock()
		guard.extraRead = extra
		guard.mu.Unlock()
		session.State, session.Frozen = "idle", false
		if err = state.Put("sessions", session.ID, session); err != nil {
			return err
		}
		changed := x.Handle(ctx, protocol.Command{ID: "changed-runtime", SourceID: "probe-input", DeviceID: c.DeviceID, SessionID: session.ID, Kind: "start", Input: []protocol.Input{{Type: "text", Text: "must be rejected before the guard"}}})
		guard.mu.Lock()
		capturedCount := len(guard.turns)
		guard.mu.Unlock()
		if changed.State != "failed" || !strings.Contains(changed.Error, "permissions or environment changed") || capturedCount != 1 {
			return errors.New("changed execution environment reached a model start")
		}
		changedRejected = true
	}
	report[fmt.Sprintf("phase%d", phase)] = map[string]any{"scopeId": session.ExecutionScopeID, "nativeScopedThreadCreated": true, "unforwardedStartsCaptured": len(turns), "attachmentViewUsed": true, "privateProfileDirectory": filepath.Dir(c.ProfilePath), "threadMethods": threads, "changedEnvironmentRejected": changedRejected, "turnUsesVerifiedActiveProfile": true}
	return nil
}
