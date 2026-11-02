package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func pausedQueue(t *testing.T, x *Executor) protocol.Session {
	t.Helper()
	created := x.Handle(context.Background(), protocol.Command{ID: "create", DeviceID: "device", ProjectID: "project", SourceID: "client", Kind: "create"})
	if created.State != "completed" {
		t.Fatal(created.Error)
	}
	stopped := x.Handle(context.Background(), protocol.Command{ID: "stop", DeviceID: "device", SessionID: created.Session.ID, SourceID: "client", Kind: "stop"})
	if stopped.State != "completed" {
		t.Fatal(stopped.Error)
	}
	return *stopped.Session
}

func addQueue(t *testing.T, x *Executor, s protocol.Session, id string, p *protocol.Policy) protocol.QueueItem {
	t.Helper()
	result := x.Handle(context.Background(), protocol.Command{ID: id, DeviceID: s.DeviceID, SessionID: s.ID, Kind: "enqueue", SourceID: "phone", Input: []protocol.Input{{Type: "text", Text: id}}, Policy: p, ExpectedPolicyVersion: s.PolicyVersion})
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	var item protocol.QueueItem
	if err := json.Unmarshal(result.Data, &item); err != nil {
		t.Fatal(err)
	}
	if item.Position < 1 {
		t.Fatal("enqueue receipt omitted its durable position")
	}
	return item
}

func TestQueueDispatchRetainsSnapshotAndDoesNotLoseFailure(t *testing.T) {
	x, s, fake := setup(t)
	session := pausedQueue(t, x)
	p := session.Policy
	p.NoTests, p.Instructions = true, "captured when queued"
	first := addQueue(t, x, session, "first", &p)
	second := addQueue(t, x, session, "second", nil)
	if first.Position >= second.Position || first.PolicyVersion == second.PolicyVersion {
		t.Fatal("queue ordering or independent policy snapshot is missing")
	}
	// Change the live draft after enqueueing; dispatch must use the captured one.
	session.Frozen, session.State = false, "idle"
	setPolicy(&session, protocol.Policy{Mode: "full", Instructions: "changed later"})
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.callErrors = map[string]error{"turn/start": errors.New("engine explicitly rejected input")}
	fake.mu.Unlock()
	x.advance(session.ID)
	items, err := s.QueueItems(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].State != "failed" || items[1].State != "queued" || items[0].Input[0].Text != "first" {
		t.Fatalf("failed dispatch lost or skipped original input: %+v", items)
	}
	fake.mu.Lock()
	for i, method := range fake.methods {
		if method == "turn/start" {
			var params map[string]any
			_ = json.Unmarshal(fake.params[i], &params)
			settings := params["collaborationMode"].(map[string]any)["settings"].(map[string]any)
			if settings["developer_instructions"] != first.PolicyInstructions {
				t.Fatal("later edits rewrote queued instructions")
			}
		}
	}
	fake.mu.Unlock()
	if err = s.Get("sessions", session.ID, &session); err != nil || !session.Frozen {
		t.Fatal("dispatch failure did not freeze later inputs")
	}
}

func TestQueueUnknownDispatchSurvivesRestartWithoutReplay(t *testing.T) {
	x, s, fake := setup(t)
	session := pausedQueue(t, x)
	first := addQueue(t, x, session, "uncertain-queue", nil)
	session.Frozen, session.State = false, "idle"
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.callErrors = map[string]error{"turn/start": &engine.UnknownOutcome{Cause: context.DeadlineExceeded}}
	fake.mu.Unlock()
	x.advance(session.ID)
	// Exercise the same startup recovery used by New without closing the fixture.
	if err := x.recoverQueue([]protocol.Session{session}); err != nil {
		t.Fatal(err)
	}
	x.advance(session.ID)
	var item protocol.QueueItem
	if err := s.Get("queue_items", first.ID, &item); err != nil || item.State != "unknown" || item.Input[0].Text != "uncertain-queue" {
		t.Fatalf("uncertain input was lost: %+v %v", item, err)
	}
	fake.mu.Lock()
	calls := 0
	for _, method := range fake.methods {
		if method == "turn/start" {
			calls++
		}
	}
	fake.mu.Unlock()
	if calls != 1 {
		t.Fatalf("uncertain queued task was dispatched %d times", calls)
	}
}

func TestQueueCancelRejectsStaleAndForeignInputs(t *testing.T) {
	x, s, _ := setup(t)
	session := pausedQueue(t, x)
	item := addQueue(t, x, session, "cancel-me", nil)
	command := protocol.Command{ID: "stale-cancel", DeviceID: "device", SessionID: session.ID, Kind: "queue.cancel", SourceID: "another-input", RequestID: item.ID, ExpectedRevision: item.Revision + 1}
	if r := x.Handle(context.Background(), command); r.State != "failed" {
		t.Fatal("stale cancellation was allowed")
	}
	command.ID, command.ExpectedRevision = "cancel", item.Revision
	if r := x.Handle(context.Background(), command); r.State != "completed" {
		t.Fatal(r.Error)
	}
	if err := s.Get("queue_items", item.ID, &item); err != nil || item.State != "cancelled" || item.Input[0].Text != "cancel-me" {
		t.Fatal("cancelling erased original input")
	}
	command.ID = "cancel-again"
	if r := x.Handle(context.Background(), command); r.State != "failed" {
		t.Fatal("old revision cancelled again")
	}
}

func TestQueueRestartFreezesUndispatchedInput(t *testing.T) {
	x, s, _ := setup(t)
	session := pausedQueue(t, x)
	item := addQueue(t, x, session, "waiting", nil)
	session.State, session.Frozen = "idle", false
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	if err := x.recoverQueue([]protocol.Session{session}); err != nil {
		t.Fatal(err)
	}
	x.advance(session.ID)
	if err := s.Get("sessions", session.ID, &session); err != nil || !session.Frozen {
		t.Fatal("restart resumed waiting work without input")
	}
	if err := s.Get("queue_items", item.ID, &item); err != nil || item.State != "queued" {
		t.Fatal("waiting input was dispatched at restart")
	}
}
