package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func finishFixtureTurn(x *Executor, fake *fixtureEngine, session protocol.Session, background bool) {
	fake.mu.Lock()
	fake.turnActive, fake.turnStatus, fake.background = false, "completed", background
	fake.mu.Unlock()
	x.consumeOne(engine.Notification{Method: "turn/completed", Params: protocol.JSON(map[string]any{"threadId": session.EngineID, "turn": map[string]string{"id": session.TurnID, "status": "completed"}})})
}

func TestQueueWaitsForBackgroundProcessWithoutRepeatingTheModelTurn(t *testing.T) {
	x, s, fake := setup(t)
	session := createAndStart(t, x)
	finishFixtureTurn(x, fake, session, true)
	var current protocol.Session
	if err := s.Get("sessions", session.ID, &current); err != nil {
		t.Fatal(err)
	}
	if current.State != "idle" || current.StopStatus != "background-running" {
		t.Fatalf("background build was hidden: %+v", current)
	}
	item := addQueue(t, x, current, "after-background", nil)
	x.advance(session.ID)
	items, err := s.QueueItems(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].State != "queued" {
		t.Fatalf("queue ran before background build exited: %+v", items)
	}
	fake.mu.Lock()
	fake.background = false
	fake.mu.Unlock()
	if x.refreshBackground(session.ID) {
		t.Fatal("exited background process remained active")
	}
	deadline := time.Now().Add(time.Second)
	for {
		items, err = s.QueueItems(session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 1 && items[0].ID == item.ID && items[0].State == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue did not continue after process exit: %+v", items)
		}
		time.Sleep(time.Millisecond)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	starts := 0
	for _, method := range fake.methods {
		if method == "turn/start" {
			starts++
		}
	}
	if starts != 2 {
		t.Fatal("background observation repeated or omitted model input")
	}
}

func TestInvalidProcessListCannotConfirmAStoppedWorkspace(t *testing.T) {
	x, s, fake := setup(t)
	session := createAndStart(t, x)
	fake.mu.Lock()
	fake.backgroundMalformed = true
	fake.mu.Unlock()
	finishFixtureTurn(x, fake, session, false)
	var current protocol.Session
	if err := s.Get("sessions", session.ID, &current); err != nil {
		t.Fatal(err)
	}
	if !current.Frozen || current.StopStatus != "processes-unchecked" {
		t.Fatal("missing process data was treated as an empty list")
	}
	if err := x.claimProject(&protocol.Session{ID: "other", ProjectID: "project"}); err == nil {
		t.Fatal("unverified process list released the workspace")
	}
	result := x.Handle(context.Background(), protocol.Command{ID: "stop-with-invalid-list", DeviceID: "device", SessionID: session.ID, SourceID: "client", Kind: "stop"})
	if result.State == "completed" {
		t.Fatal("stop accepted an invalid process response")
	}
}

func TestLateTurnEventsCannotReleaseAnUnboundUnknownStart(t *testing.T) {
	x, s, _ := setup(t)
	session := createAndStart(t, x)
	session.State, session.Frozen, session.LastStartID, session.TurnID = "unknown", true, "new-unconfirmed-start", ""
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"turn/started", "turn/completed"} {
		x.consumeOne(engine.Notification{Method: method, Params: protocol.JSON(map[string]any{"threadId": session.EngineID, "turn": map[string]string{"id": "an-older-turn", "status": "completed"}})})
	}
	var current protocol.Session
	if err := s.Get("sessions", session.ID, &current); err != nil {
		t.Fatal(err)
	}
	if current.State != "unknown" || !current.Frozen || current.TurnID != "" {
		t.Fatalf("stale event claimed an unknown input: %+v", current)
	}
	if err := x.claimProject(&protocol.Session{ID: "other", ProjectID: "project"}); err == nil {
		t.Fatal("stale completion unlocked an unknown turn")
	}
}

func TestRejectedFollowupDoesNotReleaseAnExistingBackgroundOwner(t *testing.T) {
	x, _, fake := setup(t)
	session := createAndStart(t, x)
	finishFixtureTurn(x, fake, session, true)
	fake.mu.Lock()
	fake.callErrors = map[string]error{"turn/start": errors.New("rejected")}
	fake.mu.Unlock()
	result := x.Handle(context.Background(), protocol.Command{ID: "rejected-followup", DeviceID: "device", SessionID: session.ID, SourceID: "client", Kind: "start", Input: []protocol.Input{{Type: "text", Text: "check progress"}}})
	if result.State != "failed" {
		t.Fatal("fixture did not reject followup")
	}
	if err := x.claimProject(&protocol.Session{ID: "other", ProjectID: "project"}); err == nil {
		t.Fatal("rejected input released the existing background job's workspace")
	}
}
