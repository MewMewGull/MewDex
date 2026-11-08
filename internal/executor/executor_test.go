package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type fixtureEngine struct {
	mu                  sync.Mutex
	methods             []string
	notifications       chan engine.Notification
	background          bool
	backgroundMalformed bool
	account             map[string]any
	responses           []json.RawMessage
	params              []json.RawMessage
	forks               int
	callErrors          map[string]error
	turnActive          bool
	turnStatus          string
	lastClientID        string
	systemError         bool
	environment         *engine.ExecutionEnvironment
	scopeResponseDrift  bool
}

func (f *fixtureEngine) Call(_ context.Context, method string, params any, output any) error {
	f.mu.Lock()
	f.methods = append(f.methods, method)
	f.params = append(f.params, protocol.JSON(params))
	if method == "thread/fork" {
		f.forks++
	}
	forkNumber := f.forks
	background := f.background
	backgroundMalformed := f.backgroundMalformed
	account := f.account
	callErr := f.callErrors[method]
	if callErr == nil {
		if method == "turn/start" {
			var input struct {
				ClientID string `json:"clientUserMessageId"`
			}
			_ = json.Unmarshal(protocol.JSON(params), &input)
			f.lastClientID, f.turnActive, f.turnStatus = input.ClientID, true, "inProgress"
		}
		if method == "turn/interrupt" {
			f.turnActive, f.turnStatus = false, "interrupted"
		}
	}
	turnActive, turnStatus, lastClientID, systemError := f.turnActive, f.turnStatus, f.lastClientID, f.systemError
	scopeResponseDrift := f.scopeResponseDrift
	f.mu.Unlock()
	if callErr != nil {
		return callErr
	}
	var value any = map[string]any{}
	switch method {
	case "account/read":
		value = map[string]any{"account": account}
	case "thread/start":
		value = map[string]any{"model": "fixture-model", "thread": map[string]string{"id": "engine-thread"}}
	case "thread/resume":
		var requested struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(protocol.JSON(params), &requested)
		value = map[string]any{"model": "fixture-model", "thread": map[string]string{"id": requested.ThreadID}}
	case "thread/fork":
		value = map[string]any{"model": "fixture-model", "thread": map[string]string{"id": fmt.Sprintf("forked-thread-%d", forkNumber)}}
	case "turn/start":
		value = map[string]any{"turn": map[string]string{"id": "engine-turn"}}
	case "thread/read":
		var input struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(protocol.JSON(params), &input)
		state := "idle"
		if turnActive {
			state = "active"
		} else if systemError {
			state = "systemError"
		}
		value = map[string]any{"thread": map[string]any{"id": input.ThreadID, "status": map[string]string{"type": state}}}
	case "thread/turns/list":
		turns := []any{}
		if turnStatus != "" {
			turns = append(turns, map[string]any{"id": "engine-turn", "status": turnStatus, "items": []any{map[string]string{"type": "userMessage", "clientId": lastClientID}}})
		}
		value = map[string]any{"data": turns}
	case "thread/backgroundTerminals/list":
		value = map[string]any{"data": []any{}}
		if background {
			value = map[string]any{"data": []any{map[string]string{"processId": "remaining-process"}}}
		}
		if backgroundMalformed {
			value = map[string]any{}
		}
	}
	if method == "thread/start" || method == "thread/resume" || method == "thread/fork" {
		var requested map[string]any
		_ = json.Unmarshal(protocol.JSON(params), &requested)
		response := value.(map[string]any)
		response["modelProvider"] = "fixture-provider"
		profile := requested["permissions"]
		if scopeResponseDrift {
			profile = ":workspace"
		}
		response["activePermissionProfile"] = map[string]any{"id": profile, "extends": nil}
		for _, key := range []string{"cwd", "approvalPolicy", "approvalsReviewer"} {
			response[key] = requested[key]
		}
	}
	if output == nil {
		return nil
	}
	return json.Unmarshal(protocol.JSON(value), output)
}
func (f *fixtureEngine) Respond(_ context.Context, _ json.RawMessage, result json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, result)
	return nil
}
func (f *fixtureEngine) Reject(context.Context, json.RawMessage, string) error { return nil }
func (f *fixtureEngine) ReadBaseInstructions(context.Context, string) (string, error) {
	return "fixture initial base instructions", nil
}
func (f *fixtureEngine) Notifications() <-chan engine.Notification { return f.notifications }
func (f *fixtureEngine) Close() error                              { close(f.notifications); return nil }
func (f *fixtureEngine) ExecutionEnvironment() (engine.ExecutionEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.environment != nil {
		return engine.ExecutionEnvironment{RuntimeReadOnly: append([]string(nil), f.environment.RuntimeReadOnly...), Private: append([]string(nil), f.environment.Private...)}, nil
	}
	program, err := os.Executable()
	return engine.ExecutionEnvironment{RuntimeReadOnly: []string{program}}, err
}

func setup(t *testing.T) (*Executor, *store.Store, *fixtureEngine) {
	t.Helper()
	return setupWithEngine(t, nil)
}

func setupWithEngine(t *testing.T, wrap func(*fixtureEngine) engine.Engine) (*Executor, *store.Store, *fixtureEngine) {
	t.Helper()
	dir := t.TempDir()
	data, project := filepath.Join(dir, "data"), filepath.Join(dir, "workspace")
	for _, path := range []string{data, project} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(filepath.Join(data, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fixtureEngine{notifications: make(chan engine.Notification, 10)}
	var adapter engine.Engine = fake
	if wrap != nil {
		adapter = wrap(fake)
	}
	x, err := New(config.Config{DeviceID: "device", DataDir: data, Projects: []protocol.Project{{ID: "project", Path: project}}}, s, adapter, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close(); s.Close() })
	return x, s, fake
}

func createAndStart(t *testing.T, x *Executor) protocol.Session {
	t.Helper()
	result := x.Handle(context.Background(), protocol.Command{ID: "create", DeviceID: "device", ProjectID: "project", Kind: "create", SourceID: "client"})
	if result.State != "completed" || result.Session == nil {
		t.Fatalf("create: %+v", result)
	}
	result = x.Handle(context.Background(), protocol.Command{ID: "start", DeviceID: "device", SessionID: result.Session.ID, Kind: "start", SourceID: "client", Input: []protocol.Input{{Type: "text", Text: "hello"}}})
	if result.State != "completed" || result.Session == nil {
		t.Fatalf("start: %+v", result)
	}
	return *result.Session
}

func TestFirstTurnDoesNotResumeMissingRollout(t *testing.T) {
	x, _, fake := setup(t)
	createAndStart(t, x)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, method := range fake.methods {
		if method == "thread/resume" {
			t.Fatal("unstarted thread has no rollout and must not be resumed")
		}
	}
}

func TestRemainingProcessCannotBecomeConfirmedStopped(t *testing.T) {
	x, s, fake := setup(t)
	session := createAndStart(t, x)
	fake.mu.Lock()
	fake.background = true
	fake.mu.Unlock()
	result := x.Handle(context.Background(), protocol.Command{ID: "stop", DeviceID: "device", SessionID: session.ID, Kind: "stop", SourceID: "client"})
	if result.State != "failed" {
		t.Fatalf("stop wrongly confirmed: %+v", result)
	}
	if !strings.Contains(result.Error, "background processes remain") {
		t.Fatalf("stop failed before process verification: %s", result.Error)
	}
	fake.notifications <- engine.Notification{Method: "turn/completed", Params: protocol.JSON(map[string]any{"threadId": "engine-thread", "turn": map[string]string{"id": "engine-turn", "status": "interrupted"}})}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var actual protocol.Session
		if err := s.Get("sessions", session.ID, &actual); err != nil {
			t.Fatal(err)
		}
		if actual.TurnID == "" {
			if actual.State != "unknown" || actual.StopStatus != "unconfirmed" {
				t.Fatalf("model cancellation hid remaining process: %+v", actual)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("completion notification was not consumed")
}

func TestStopFreezesQueuedMessages(t *testing.T) {
	x, s, fake := setup(t)
	session := createAndStart(t, x)
	queued := x.Handle(context.Background(), protocol.Command{ID: "queued", DeviceID: "device", SessionID: session.ID, Kind: "enqueue", SourceID: "client", Input: []protocol.Input{{Type: "text", Text: "later"}}})
	if queued.State != "completed" {
		t.Fatal(queued.Error)
	}
	stopped := x.Handle(context.Background(), protocol.Command{ID: "stop", DeviceID: "device", SessionID: session.ID, Kind: "stop", SourceID: "client"})
	if stopped.State != "completed" {
		t.Fatal(stopped.Error)
	}
	fake.notifications <- engine.Notification{Method: "turn/completed", Params: protocol.JSON(map[string]any{"threadId": "engine-thread", "turn": map[string]string{"id": "engine-turn", "status": "interrupted"}})}
	time.Sleep(25 * time.Millisecond)
	var actual protocol.Session
	if err := s.Get("sessions", session.ID, &actual); err != nil {
		t.Fatal(err)
	}
	if !actual.Frozen || actual.State != "paused" {
		t.Fatalf("queue not frozen: %+v", actual)
	}
	queue, err := s.QueueItems(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("queued command was consumed after stop: %d", len(queue))
	}
	if queue[0].State != "queued" {
		t.Fatalf("stopped queue changed state: %s", queue[0].State)
	}
}
