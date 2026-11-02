package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func recoveryEvidence(device string) store.RestartEvidence {
	return store.RestartEvidence{ID: "review", DeviceID: device, BootSession: "verified-new-boot", RestartFrom: "old-boot", PlanSHA256: "prepared-local-scope"}
}

func TestRetireOldExecutionReleasesProjectWithoutResolvingOrReplayingInput(t *testing.T) {
	x, state, fake := setup(t)
	ctx := context.Background()
	session := createAndStart(t, x)
	oldCommand := protocol.Command{ID: "uncertain-original", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "start", SourceID: "phone", Input: []protocol.Input{{Type: "text", Text: "original input stays unknown"}}}
	if _, _, err := state.Claim(oldCommand); err != nil {
		t.Fatal(err)
	}
	if err := state.Finish(protocol.CommandResult{CommandID: oldCommand.ID, State: "unknown", Error: "lost original outcome"}); err != nil {
		t.Fatal(err)
	}
	session.State, session.Frozen, session.StopStatus, session.LastStartID = "unknown", true, "unconfirmed", oldCommand.ID
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	queued := protocol.QueueItem{ID: "original-queued", DeviceID: session.DeviceID, SessionID: session.ID, ProjectID: session.ProjectID, Revision: 1, State: "queued", Input: []protocol.Input{{Type: "text", Text: "do not dispatch this preserved input"}}}
	queued, _, err := state.PutQueueItem(queued)
	if err != nil {
		t.Fatal(err)
	}
	if err = state.CaptureRestartRecovery(ctx, recoveryEvidence(session.DeviceID)); err != nil {
		t.Fatal(err)
	}
	if err = x.save(&session); err != nil || session.ExecutionRecovery == nil {
		t.Fatal(session, err)
	}
	newSession := x.Handle(ctx, protocol.Command{ID: "next-conversation", DeviceID: session.DeviceID, ProjectID: session.ProjectID, Kind: "create", SourceID: "web"})
	if newSession.State != "completed" {
		t.Fatal(newSession)
	}
	start := protocol.Command{ID: "blocked-new-task", DeviceID: session.DeviceID, SessionID: newSession.Session.ID, Kind: "start", SourceID: "web", Input: []protocol.Input{{Type: "text", Text: "a new task"}}}
	if result := x.Handle(ctx, start); result.State != "failed" {
		t.Fatal("unconfirmed project was not held", result)
	}
	fake.mu.Lock()
	before := len(fake.methods)
	fake.mu.Unlock()
	retire := protocol.Command{ID: "retire", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "execution.retire", RequestID: session.ExecutionRecovery.ID, SourceID: "phone"}
	result := x.Handle(ctx, retire)
	if result.State != "completed" || result.Session.ExecutionRecovery.State != "retired" || result.Session.State != "unknown" || result.Session.TurnID != session.TurnID || !result.Session.Frozen {
		t.Fatal(result)
	}
	fake.mu.Lock()
	after := len(fake.methods)
	fake.mu.Unlock()
	if after != before {
		t.Fatal("retirement made an engine call")
	}
	if receipt, err := state.Result(oldCommand.ID); err != nil || receipt.State != "unknown" || receipt.Error != "lost original outcome" {
		t.Fatal("original receipt was resolved without evidence", receipt, err)
	}
	var kept protocol.QueueItem
	if err = state.Get("queue_items", queued.ID, &kept); err != nil || string(protocol.JSON(kept)) != string(protocol.JSON(queued)) {
		t.Fatal("retirement rewrote queued input", err)
	}
	for _, kind := range []string{"start", "resume", "enqueue", "stop", "reconcile"} {
		blocked := retire
		blocked.ID = "old-" + kind
		blocked.Kind = kind
		blocked.Input = []protocol.Input{{Type: "text", Text: "never replay"}}
		if reply := x.Handle(ctx, blocked); reply.State != "failed" {
			t.Fatal("retired execution accepted a new action", kind, reply)
		}
	}
	if repeated := x.Handle(ctx, retire); repeated.State != "completed" {
		t.Fatal(repeated)
	}
	retired := result.Session
	eventsBefore, err := state.Events(0, session.ID, 1000)
	if err != nil {
		t.Fatal(err)
	}
	x.consumeOne(engine.Notification{Method: "turn/completed", Params: protocol.JSON(map[string]any{"threadId": session.EngineID, "turn": map[string]string{"id": session.TurnID, "status": "completed"}})})
	x.consumeOne(engine.Notification{Method: "item/agentMessage/delta", Params: protocol.JSON(map[string]string{"threadId": session.EngineID, "delta": "late output"})})
	var afterLate protocol.Session
	if err = state.Get("sessions", session.ID, &afterLate); err != nil || string(protocol.JSON(afterLate)) != string(protocol.JSON(retired)) {
		t.Fatal("late events rewrote retired history", afterLate, err)
	}
	eventsAfter, err := state.Events(0, session.ID, 1000)
	if err != nil || len(eventsBefore) != len(eventsAfter) {
		t.Fatal("late output entered retired history", err)
	}
	start.ID = "explicit-new-task"
	if reply := x.Handle(ctx, start); reply.State != "completed" {
		t.Fatal("retirement left the old project claim active", reply)
	}
	// Reusing the old public review on this newer execution cannot release it.
	foreign := retire
	foreign.ID = "forged-new-retirement"
	foreign.SessionID = newSession.Session.ID
	if reply := x.Handle(ctx, foreign); reply.State != "failed" {
		t.Fatal("new attempt inherited the old restart certificate", reply)
	}
}

func TestUnconfirmedStopKeepsTheProjectClaimAfterExecutorRestart(t *testing.T) {
	data, project := t.TempDir(), t.TempDir()
	state, err := store.Open(filepath.Join(data, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	old := protocol.Session{ID: "old", DeviceID: "device", ProjectID: "project", EngineID: "old-thread", EngineStarted: true, State: "idle", StopStatus: "unconfirmed", LastStartID: "old-start"}
	if err = state.Put("sessions", old.ID, old); err != nil {
		t.Fatal(err)
	}
	fake := &fixtureEngine{notifications: make(chan engine.Notification, 10)}
	x, err := New(config.Config{DeviceID: old.DeviceID, DataDir: data, Projects: []protocol.Project{{ID: old.ProjectID, Path: project}}}, state, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	if len(x.workspaceOwners) != 1 {
		t.Fatal("an unconfirmed stop lost its project claim on restart")
	}
	var reloaded protocol.Session
	if err = state.Get("sessions", old.ID, &reloaded); err != nil || reloaded.State != "unknown" || !reloaded.Frozen {
		t.Fatal("unconfirmed stop was treated as resolved", reloaded, err)
	}
	if len(fake.methods) != 0 {
		t.Fatal("restart submitted work to the engine")
	}
}

func TestRetiredUnknownQueueDoesNotRestoreAProjectClaimOnRestart(t *testing.T) {
	root := t.TempDir()
	data, project := filepath.Join(root, "data"), filepath.Join(root, "project")
	for _, dir := range []string{data, project} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.Open(filepath.Join(data, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	old := protocol.Session{ID: "old", DeviceID: "device", ProjectID: "project", EngineID: "old-thread", EngineStarted: true, State: "unknown", StopStatus: "unconfirmed", Frozen: true, LastStartID: "old-start", TurnID: "old-turn", Model: "fixture-model"}
	if err = state.Put("sessions", old.ID, old); err != nil {
		t.Fatal(err)
	}
	if _, _, err = state.PutQueueItem(protocol.QueueItem{ID: "unknown-old-input", SessionID: old.ID, DeviceID: old.DeviceID, ProjectID: old.ProjectID, State: "unknown", Revision: 1, Input: []protocol.Input{{Type: "text", Text: "outcome stays unknown"}}}); err != nil {
		t.Fatal(err)
	}
	if err = state.CaptureRestartRecovery(context.Background(), recoveryEvidence(old.DeviceID)); err != nil {
		t.Fatal(err)
	}
	view, err := state.ExecutionRecovery(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = state.RetireExecution(old, protocol.Command{ID: "retire-before-restart", DeviceID: old.DeviceID, SessionID: old.ID, SourceID: "phone", Kind: "execution.retire", RequestID: view.ID}); err != nil {
		t.Fatal(err)
	}
	fake := &fixtureEngine{notifications: make(chan engine.Notification, 10)}
	x, err := New(config.Config{DeviceID: "device", DataDir: data, Projects: []protocol.Project{{ID: "project", Path: project}}}, state, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	if len(fake.methods) != 0 {
		t.Fatal("restart resumed archived work")
	}
	if len(x.workspaceOwners) != 0 {
		t.Fatal("retired unknown input restored its project claim")
	}
	var reloaded protocol.Session
	if err = state.Get("sessions", old.ID, &reloaded); err != nil || reloaded.ExecutionRecovery == nil || reloaded.ExecutionRecovery.State != "retired" || reloaded.State != "unknown" {
		t.Fatal(reloaded, err)
	}
	var queue protocol.QueueItem
	if err = state.Get("queue_items", "unknown-old-input", &queue); err != nil || queue.State != "unknown" {
		t.Fatal(queue, err)
	}
}
