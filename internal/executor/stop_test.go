package executor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func TestStopBindsUncertainQueuedTurnWithoutStartingItAgain(t *testing.T) {
	x, store, fake := setup(t)
	session := pausedQueue(t, x)
	item := addQueue(t, x, session, "uncertain", nil)
	session.State, session.Frozen = "idle", false
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.callErrors = map[string]error{"turn/start": &engine.UnknownOutcome{Cause: context.DeadlineExceeded}}
	fake.mu.Unlock()
	x.advance(session.ID)
	// The request reached the engine, but its response and notification were lost.
	fake.mu.Lock()
	fake.turnActive, fake.turnStatus, fake.lastClientID = true, "inProgress", item.ID
	fake.mu.Unlock()
	r := x.Handle(context.Background(), protocol.Command{ID: "stop-uncertain", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "stop", SourceID: "phone"})
	if r.State != "completed" || r.Session.State != "paused" || r.Session.StopStatus != "registered-processes-cleared" {
		t.Fatalf("%+v", r)
	}
	if err := store.Get("queue_items", item.ID, &item); err != nil || item.State != "interrupted" || item.TurnID != "engine-turn" {
		t.Fatalf("%+v %v", item, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	starts := 0
	for _, method := range fake.methods {
		if method == "turn/start" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatal("stop replayed the uncertain start")
	}
}

func TestMissingTurnIDDoesNotBecomeConfirmedStoppedWithoutEvidence(t *testing.T) {
	x, store, _ := setup(t)
	session := pausedQueue(t, x)
	session.EngineStarted, session.State, session.LastStartID, session.StopStatus = true, "unknown", "unobserved", "unconfirmed"
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	r := x.Handle(context.Background(), protocol.Command{ID: "stop-missing", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "stop", SourceID: "phone"})
	if r.State != "failed" {
		t.Fatalf("stop without history was confirmed: %+v", r)
	}
	if err := store.Get("sessions", session.ID, &session); err != nil || session.State != "unknown" || !session.Frozen {
		t.Fatal("missing engine history was treated as stopped")
	}
}

func TestReconcileReadsOriginalTurnWithoutInterruptingOrResubmitting(t *testing.T) {
	x, store, fake := setup(t)
	session := createAndStart(t, x)
	session.State, session.TurnID, session.Frozen, session.StopStatus = "unknown", "", true, "unconfirmed"
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	r := x.Handle(context.Background(), protocol.Command{ID: "observe", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "reconcile", SourceID: "web"})
	if r.State != "completed" || r.Session.State != "running" || r.Session.TurnID != "engine-turn" {
		t.Fatalf("%+v", r)
	}
	fake.mu.Lock()
	starts, interrupts := 0, 0
	for _, method := range fake.methods {
		if method == "turn/start" {
			starts++
		}
		if method == "turn/interrupt" {
			interrupts++
		}
	}
	fake.mu.Unlock()
	if starts != 1 || interrupts != 0 {
		t.Fatal("observation caused an execution")
	}
	if err := store.Get("sessions", session.ID, &session); err != nil || !session.Frozen {
		t.Fatal("observation unfroze the queue")
	}
}

type blockedSteer struct {
	*fixtureEngine
	entered chan struct{}
}

func (b *blockedSteer) Call(ctx context.Context, method string, params any, output any) error {
	if method == "turn/steer" {
		close(b.entered)
		<-ctx.Done()
		return &engine.UnknownOutcome{Cause: ctx.Err()}
	}
	return b.fixtureEngine.Call(ctx, method, params, output)
}

func TestStopCancelsOrdinaryRPCWaitBeforeTakingSessionLock(t *testing.T) {
	var b *blockedSteer
	x, _, _ := setupWithEngine(t, func(fake *fixtureEngine) engine.Engine {
		b = &blockedSteer{fixtureEngine: fake, entered: make(chan struct{})}
		return b
	})
	session := createAndStart(t, x)
	steered := make(chan protocol.CommandResult, 1)
	go func() {
		steered <- x.Handle(context.Background(), protocol.Command{ID: "waiting-steer", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "steer", SourceID: "desktop", Input: []protocol.Input{{Type: "text", Text: "adjust"}}})
	}()
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("steer did not reach its RPC")
	}
	stopped := make(chan protocol.CommandResult, 1)
	go func() {
		stopped <- x.Handle(context.Background(), protocol.Command{ID: "priority-stop", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "stop", SourceID: "phone"})
	}()
	select {
	case r := <-stopped:
		if r.State != "completed" || r.Session.State != "paused" {
			t.Fatalf("%+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("stop waited behind the ordinary RPC timeout")
	}
	if r := <-steered; r.State != "unknown" {
		t.Fatalf("cancelled write lost its unknown outcome: %+v", r)
	}
}

func TestIdleEngineWithUnfinalizedHistoryIsNotResumable(t *testing.T) {
	x, store, fake := setup(t)
	session := createAndStart(t, x)
	session.State, session.Frozen = "unknown", true
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.turnActive = false
	fake.mu.Unlock()
	r := x.Handle(context.Background(), protocol.Command{ID: "read-stale-history", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "reconcile", SourceID: "client"})
	if r.State != "failed" {
		t.Fatal("unfinalized history was treated as completed")
	}
	var view map[string]any
	_ = json.Unmarshal(r.Data, &view)
	if err := store.Get("sessions", session.ID, &session); err != nil || !session.Frozen || session.State != "unknown" {
		t.Fatal("unsafe observation changed session state")
	}
}

func TestFinalizedFailedTurnCanBeObservedInSystemErrorState(t *testing.T) {
	x, s, fake := setup(t)
	session := createAndStart(t, x)
	session.State, session.TurnID, session.Frozen = "unknown", "", true
	if err := x.save(&session); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.turnActive, fake.systemError, fake.turnStatus = false, true, "failed"
	fake.mu.Unlock()
	r := x.Handle(context.Background(), protocol.Command{ID: "inspect-system-error", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "reconcile", SourceID: "client"})
	if r.State != "completed" || r.Session.State != "paused" || r.Session.StopStatus != "processes-unchecked" {
		t.Fatalf("%+v", r)
	}
	resume := x.Handle(context.Background(), protocol.Command{ID: "unsafe-resume", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "resume", SourceID: "client"})
	if resume.State != "failed" {
		t.Fatal("observing a failed turn implicitly confirmed process cleanup")
	}
	if err := s.Get("sessions", session.ID, &session); err != nil || !session.Frozen {
		t.Fatal("observation resumed the queue")
	}
}
