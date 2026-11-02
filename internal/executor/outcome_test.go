package executor

import (
	"context"
	"testing"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func TestUncertainEngineRequestIsNeverReplayed(t *testing.T) {
	x, store, fake := setup(t)
	r := x.Handle(context.Background(), protocol.Command{ID: "create", SourceID: "client", DeviceID: "device", ProjectID: "project", Kind: "create"})
	if r.State != "completed" {
		t.Fatal(r.Error)
	}
	fake.mu.Lock()
	fake.callErrors = map[string]error{"turn/start": &engine.UnknownOutcome{Cause: context.DeadlineExceeded}}
	fake.mu.Unlock()
	command := protocol.Command{ID: "uncertain", SourceID: "client", DeviceID: "device", SessionID: r.Session.ID, Kind: "start", Input: []protocol.Input{{Type: "text", Text: "do once"}}}
	for i := 0; i < 2; i++ {
		if result := x.Handle(context.Background(), command); result.State != "unknown" {
			t.Fatalf("uncertain operation reported as %s", result.State)
		}
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
		t.Fatalf("unknown operation replayed %d times", calls)
	}
	var session protocol.Session
	if err := store.Get("sessions", command.SessionID, &session); err != nil {
		t.Fatal(err)
	}
	if session.State != "unknown" || !session.Frozen {
		t.Fatal("uncertain task was allowed to continue automatically")
	}
}
