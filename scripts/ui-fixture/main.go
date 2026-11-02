// ui-fixture exercises the normal executor, store and transport with labelled
// synthetic history. Its engine refuses every call; it cannot run a model/tool.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"mewdex/internal/transport"
)

type inertEngine struct {
	channel chan engine.Notification
	once    sync.Once
	calls   atomic.Int32
}

func (e *inertEngine) Call(_ context.Context, method string, _ any, _ any) error {
	e.calls.Add(1)
	return fmt.Errorf("UI fixture refuses engine method %s; no model or tool can run", method)
}
func (*inertEngine) Respond(context.Context, json.RawMessage, json.RawMessage) error {
	return errors.New("UI fixture has no upstream interaction")
}
func (*inertEngine) Reject(context.Context, json.RawMessage, string) error {
	return errors.New("UI fixture has no upstream interaction")
}
func (e *inertEngine) Notifications() <-chan engine.Notification { return e.channel }
func (e *inertEngine) Close() error                              { e.once.Do(func() { close(e.channel) }); return nil }
func (*inertEngine) ExecutionEnvironment() (engine.ExecutionEnvironment, error) {
	return engine.ExecutionEnvironment{}, errors.New("UI fixture does not authorize execution scope creation")
}

func eventID(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:16])
}

func run() error {
	profile := flag.String("config", ".state/ui-fixture/device.json", "owned UI fixture profile")
	alerts := flag.Bool("notification-events", false, "accept at most four explicit synthetic completion signals")
	recovery := flag.Bool("execution-recovery", false, "use a separately labelled old execution with simulated restart evidence")
	standalone := flag.Bool("standalone", false, "serve an isolated loopback Hub and a scoped input invitation")
	flag.Parse()
	c, err := config.Load(*profile)
	if err != nil {
		return err
	}
	if c.Name != "界面检查 · 模拟记录（无模型）" || c.Hub || !c.Executor || c.Gateway != nil || len(c.Projects) != 1 || c.Engine.UseHubGateway {
		return errors.New("refusing a profile that is not the isolated UI fixture")
	}
	lock, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	s, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	sessionID := "mewdex-ui-history-20261028"
	if *recovery {
		sessionID = "mewdex-ui-recovery-20261029"
	}
	var session protocol.Session
	if err = s.Get("sessions", sessionID, &session); errors.Is(err, store.ErrNotFound) {
		session = protocol.Session{ID: sessionID, DeviceID: c.DeviceID, ProjectID: c.Projects[0].ID, Title: "界面检查：历史与冻结队列（模拟记录）", State: "paused", Frozen: true, StopStatus: "not-started", Policy: protocol.Policy{Mode: "focused", Completion: "仅界面检查，不启动模型或工具", Instructions: "检查数据为明确标记的模拟记录；保持队列冻结。"}, CreatedAt: protocol.Now(), UpdatedAt: protocol.Now()}
		if *recovery {
			session.Title, session.State, session.StopStatus, session.LastStartID = "界面检查：结束旧执行（模拟重启证据）", "unknown", "unconfirmed", "ui-recovery-original-input"
		}
		if err = s.Put("sessions", sessionID, session); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if session.DeviceID != c.DeviceID || session.EngineID != "" || !session.Frozen || (session.State != "paused" && session.State != "unknown") {
		return errors.New("existing fixture session is not safely frozen")
	}
	old, err := s.Events(0, sessionID, 10000)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, event := range old {
		seen[event.ID] = true
	}
	appendEvent := func(id, kind string, payload any) error {
		if *recovery {
			id = sessionID + ":" + id
		}
		id = eventID(id)
		if seen[id] {
			return nil
		}
		_, err := s.Append(protocol.Event{ID: id, DeviceID: c.DeviceID, SessionID: sessionID, Kind: kind, Payload: protocol.JSON(payload), CreatedAt: protocol.Now()})
		return err
	}
	if err = appendEvent("ui-fixture-session", "session.updated", session); err != nil {
		return err
	}
	if *recovery {
		if err := seedExecutionRecovery(s, session); err != nil {
			return err
		}
	}
	for i := 1; i <= 120; i++ {
		if err = appendEvent(fmt.Sprintf("ui-user-%03d", i), "message.user", map[string]any{"input": []protocol.Input{{Type: "text", Text: fmt.Sprintf("界面检查模拟输入 %03d（未发送给模型）", i)}}}); err != nil {
			return err
		}
		if err = appendEvent(fmt.Sprintf("ui-assistant-%03d", i), "engine.item/completed", map[string]any{"item": map[string]string{"id": fmt.Sprintf("ui-output-%03d", i), "type": "agentMessage", "text": fmt.Sprintf("界面检查模拟回复 %03d；仅用于历史分页，不表示模型执行。", i)}}); err != nil {
			return err
		}
	}
	e := &inertEngine{channel: make(chan engine.Notification)}
	x, err := executor.New(c, s, e, nil)
	if err != nil {
		return err
	}
	defer x.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var alertErr error
	var alertDone sync.WaitGroup
	instance := protocol.ID()
	root := filepath.Dir(*profile)
	record := map[string]any{"pid": os.Getpid(), "instanceId": instance, "deviceId": c.DeviceID, "sessionId": sessionID, "state": "running", "modelCalls": 0, "historySynthetic": true, "finalCandidate": false}
	if *recovery {
		record["restartEvidenceSimulated"], record["actualOSRestartPerformed"] = true, false
	}
	save := func() error {
		raw, _ := json.MarshalIndent(record, "", "  ")
		return os.WriteFile(filepath.Join(root, "runtime.json"), raw, 0600)
	}
	if err = save(); err != nil {
		return err
	}
	alertDone.Add(1)
	go func() {
		defer alertDone.Done()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if *alerts {
					if err := fixtureAlerts(s, root, instance, c.DeviceID, sessionID); err != nil {
						alertErr = err
						cancel()
						return
					}
				}
				if _, err := os.Stat(filepath.Join(root, "stop-"+instance)); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	n := &transport.Node{Config: c, Store: s, Executor: x, Device: protocol.Device{ID: c.DeviceID, Name: c.Name, Platform: runtime.GOOS + "/" + runtime.GOARCH, Roles: []string{"executor"}, Projects: c.Projects, ProtocolVersion: protocol.Version}}
	fmt.Println("UI fixture starting through the normal transport; synthetic history is labelled and all engine calls are refused")
	if *standalone {
		err = serveStandalone(ctx, c, s, x, n.Device, root)
	} else {
		err = n.Run(ctx)
	}
	cancel()
	alertDone.Wait()
	record["state"], record["modelCalls"] = "stopped", e.calls.Load()
	if alertErr != nil {
		record["state"], record["error"] = "failed", alertErr.Error()
	}
	if saveErr := save(); saveErr != nil {
		return saveErr
	}
	if alertErr != nil {
		return alertErr
	}
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func seedExecutionRecovery(s *store.Store, session protocol.Session) error {
	command := protocol.Command{ID: session.LastStartID, DeviceID: session.DeviceID, SessionID: session.ID, SourceID: "synthetic-ui-input", Kind: "start", Input: []protocol.Input{{Type: "text", Text: "模拟原输入；结果保持未知，不能重新发送"}}}
	if _, owner, err := s.Claim(command); err != nil {
		return err
	} else if owner {
		if err := s.Finish(protocol.CommandResult{CommandID: command.ID, State: "unknown", Error: "synthetic lost result; no model was called"}); err != nil {
			return err
		}
	}
	for _, state := range []string{"queued", "unknown"} {
		id := "ui-recovery-" + state
		var existing protocol.QueueItem
		if err := s.Get("queue_items", id, &existing); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if _, _, err := s.PutQueueItem(protocol.QueueItem{ID: id, DeviceID: session.DeviceID, SessionID: session.ID, ProjectID: session.ProjectID, Revision: 1, State: state, Policy: session.Policy, Input: []protocol.Input{{Type: "text", Text: "模拟保留输入 · " + state}}, CreatedAt: protocol.Now(), UpdatedAt: protocol.Now()}); err != nil {
			return err
		}
	}
	// This script is never an application resource. The production CLI and input
	// protocol cannot supply boot evidence; these values are labelled synthetic.
	return s.CaptureRestartRecovery(context.Background(), store.RestartEvidence{ID: "ui-synthetic-restart-20261029", DeviceID: session.DeviceID, BootSession: "simulated-after", RestartFrom: "simulated-before", PlanSHA256: "UI fixture only; no actual restart"})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
