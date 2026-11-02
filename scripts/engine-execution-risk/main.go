// Probe real pinned-engine history correlation with an isolated unsigned home.
// Deliberately loses one start receipt and lifecycle notifications; never replays.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type lostReceipt struct {
	engine.Engine
	notifications chan engine.Notification
	completed     chan struct{}
	starts        atomic.Int32
}

func (e *lostReceipt) Call(ctx context.Context, method string, params, out any) error {
	if method == "turn/start" {
		e.starts.Add(1)
		if err := e.Engine.Call(ctx, method, params, out); err != nil {
			return err
		}
		return &engine.UnknownOutcome{Cause: errors.New("development probe deliberately discarded the receipt")}
	}
	return e.Engine.Call(ctx, method, params, out)
}
func (e *lostReceipt) Notifications() <-chan engine.Notification { return e.notifications }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	resume := flag.String("resume", "", "reinspect this existing isolated probe without submitting another turn")
	flag.Parse()
	for _, name := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "AZURE_OPENAI_API_KEY", "OPENAI_BASE_URL"} {
		if os.Getenv(name) != "" {
			return errors.New("provider environment is present; unsigned probe was not started")
		}
	}
	cfg, err := config.Load(".local/device.json")
	if err != nil {
		return err
	}
	root := *resume
	if root == "" {
		root, err = os.MkdirTemp(".state", "engine-execution-")
		if err != nil {
			return err
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	if *resume != "" {
		base, e := filepath.Abs(".state")
		if e != nil {
			return e
		}
		root, e = filepath.EvalSymlinks(root)
		if e != nil {
			return e
		}
		base, e = filepath.EvalSymlinks(base)
		if e != nil {
			return e
		}
		if !strings.HasPrefix(filepath.Base(root), "engine-execution-") || filepath.Dir(root) != base {
			return errors.New("resume requires an existing project-local execution probe")
		}
	}
	workspace := filepath.Join(root, "workspace")
	if err = os.MkdirAll(workspace, 0700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	real, err := engine.Start(ctx, config.Engine{Executable: cfg.Engine.Executable, Args: cfg.Engine.Args, Home: filepath.Join(root, "engine-home")})
	if err != nil {
		return err
	}
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if err = real.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		real.Close()
		return err
	}
	if len(account.Account) > 0 && string(account.Account) != "null" {
		real.Close()
		return errors.New("isolated engine unexpectedly has an account; no turn submitted")
	}
	wrapped := &lostReceipt{Engine: real, notifications: make(chan engine.Notification, 100), completed: make(chan struct{}, 1)}
	go func() {
		defer close(wrapped.notifications)
		for n := range real.Notifications() {
			if len(n.RequestID) > 0 {
				_ = real.Reject(ctx, n.RequestID, "No tools are authorized in this development probe")
				continue
			}
			if n.Method == "turn/completed" {
				select {
				case wrapped.completed <- struct{}{}:
				default:
				}
				continue
			}
			if n.Method == "turn/started" {
				continue
			}
			wrapped.notifications <- n
		}
	}()
	s, err := store.Open(filepath.Join(root, "executor.db"))
	if err != nil {
		real.Close()
		return err
	}
	defer s.Close()
	x, err := executor.New(config.Config{DeviceID: "isolated-risk", DataDir: root, Projects: []protocol.Project{{ID: "risk", Path: workspace}}}, s, wrapped, nil)
	if err != nil {
		real.Close()
		return err
	}
	defer x.Close()
	startID, sessionID := "", ""
	if *resume == "" {
		created := x.Handle(ctx, protocol.Command{ID: protocol.ID(), DeviceID: "isolated-risk", ProjectID: "risk", Kind: "create", SourceID: "risk-input", Policy: &protocol.Policy{Mode: "edit", NoTests: true, Instructions: "No tools or inference authorization is provided in this unsigned development probe."}})
		if created.State != "completed" {
			return errors.New(created.Error)
		}
		startID, sessionID = protocol.ID(), created.Session.ID
		started := x.Handle(ctx, protocol.Command{ID: startID, DeviceID: "isolated-risk", SessionID: sessionID, Kind: "start", SourceID: "risk-input", Input: []protocol.Input{{Type: "text", Text: "Unsigned execution-state correlation probe. Do not run tools."}}})
		if started.State != "unknown" {
			return fmt.Errorf("expected discarded start receipt, got %s: %s", started.State, started.Error)
		}
		select {
		case <-wrapped.completed:
		case <-ctx.Done():
			return ctx.Err()
		}
	} else {
		sessions, err := store.List[protocol.Session](s, "sessions")
		if err != nil {
			return err
		}
		if len(sessions) != 1 || sessions[0].DeviceID != "isolated-risk" || sessions[0].LastStartID == "" {
			return errors.New("existing path is not an execution probe")
		}
		sessionID, startID = sessions[0].ID, sessions[0].LastStartID
	}
	var before protocol.Session
	if err = s.Get("sessions", sessionID, &before); err != nil {
		return err
	}
	observed := x.Handle(ctx, protocol.Command{ID: protocol.ID(), DeviceID: "isolated-risk", SessionID: before.ID, Kind: "reconcile", SourceID: "risk-input"})
	if observed.State != "completed" {
		var metadata struct {
			Thread struct {
				ID     string          `json:"id"`
				Status json.RawMessage `json:"status"`
			} `json:"thread"`
		}
		_ = real.Call(ctx, "thread/read", map[string]any{"threadId": before.EngineID, "includeTurns": false}, &metadata)
		_ = os.WriteFile(".state/evidence/engine-execution-recovery-diagnostic.json", protocol.JSON(map[string]any{"probeDirectory": root, "metadata": metadata, "error": observed.Error}), 0600)
		return errors.New(observed.Error)
	}
	var view protocol.ExecutionObservation
	if err = json.Unmarshal(observed.Data, &view); err != nil {
		return err
	}
	if !view.Matched || view.ClientID != startID || view.TurnID == "" || view.TurnStatus != "failed" {
		return fmt.Errorf("engine history correlation failed: %+v", view)
	}
	receipt, err := s.Result(startID)
	if err != nil {
		return err
	}
	stopped := x.Handle(ctx, protocol.Command{ID: protocol.ID(), DeviceID: "isolated-risk", SessionID: before.ID, Kind: "stop", SourceID: "risk-input"})
	if stopped.State != "completed" {
		return errors.New(stopped.Error)
	}
	report := map[string]any{"at": protocol.Now(), "probeDirectory": root, "isolatedUnsignedAccount": true, "existingProbeReadOnly": *resume != "", "beforeState": before.State, "beforeTurnIDMissing": before.TurnID == "", "observation": view, "startReceiptRecovered": receipt.State == "completed", "stoppedState": stopped.Session.State, "stopStatus": stopped.Session.StopStatus, "frozen": stopped.Session.Frozen, "startCallsThisRun": wrapped.starts.Load(), "inferenceRun": false, "toolsAuthorized": false}
	expectedStarts := int32(1)
	if *resume != "" {
		expectedStarts = 0
	}
	if wrapped.starts.Load() != expectedStarts || stopped.Session.State != "paused" || !stopped.Session.Frozen {
		return errors.New("execution probe crossed its no-replay boundary")
	}
	if err = os.WriteFile(".state/evidence/engine-execution-recovery.json", protocol.JSON(report), 0600); err != nil {
		return err
	}
	fmt.Println(string(protocol.JSON(report)))
	return nil
}
