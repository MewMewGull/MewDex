package executor

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestTwoInputsCannotAnswerSameCallbackTwice(t *testing.T) {
	x, _, fake := setup(t)
	session := createAndStart(t, x)
	x.receiveRequest(engine.Notification{Method: "item/tool/requestUserInput", RequestID: json.RawMessage(`7`), Params: protocol.JSON(map[string]any{"threadId": session.EngineID, "turnId": session.TurnID, "isBlocking": true, "questions": []map[string]string{{"id": "q", "header": "任务", "question": "选择哪一个方案"}}})})
	requests := x.pendingInteractions(session.ID)
	if len(requests) != 1 {
		t.Fatalf("pending requests: %d", len(requests))
	}
	results := make(chan protocol.CommandResult, 2)
	var wg sync.WaitGroup
	for _, input := range []string{"phone", "desktop"} {
		wg.Add(1)
		go func(source string) {
			defer wg.Done()
			results <- x.Handle(context.Background(), protocol.Command{ID: "reply-" + source, SourceID: source, DeviceID: session.DeviceID, SessionID: session.ID, Kind: "respond", RequestID: requests[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept", Answers: map[string][]string{"q": {"方案一"}}})})
		}(input)
	}
	wg.Wait()
	close(results)
	completed := 0
	for r := range results {
		if r.State == "completed" {
			completed++
		}
	}
	if completed != 1 {
		t.Fatalf("accepted %d replies", completed)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.responses) != 1 || string(fake.responses[0]) != `{"answers":{"q":{"answers":["方案一"]}}}` {
		t.Fatalf("engine responses: %s", fake.responses)
	}
}

func TestStoppedTaskCannotAcceptAnOutstandingApproval(t *testing.T) {
	x, _, fake := setup(t)
	session := createAndStart(t, x)
	x.receiveRequest(engine.Notification{Method: "item/fileChange/requestApproval", RequestID: json.RawMessage(`"file"`), Params: protocol.JSON(map[string]string{"threadId": session.EngineID, "turnId": session.TurnID})})
	requests := x.pendingInteractions(session.ID)
	x.Handle(context.Background(), protocol.Command{ID: "stop", SourceID: "phone", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "stop"})
	r := x.Handle(context.Background(), protocol.Command{ID: "late-accept", SourceID: "other", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "respond", RequestID: requests[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
	if r.State != "failed" {
		t.Fatal("stopped task accepted an operation")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.responses) != 0 {
		t.Fatal("approval escaped to stopped engine")
	}
}

func TestRestartExpiresStoredCallbacks(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Put("interactions", "old", protocol.Interaction{ID: "old", SessionID: "s", State: "pending"}); err != nil {
		t.Fatal(err)
	}
	fake := &fixtureEngine{notifications: make(chan engine.Notification, 1)}
	x, err := New(config.Config{DeviceID: "device", DataDir: dir}, s, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	var request protocol.Interaction
	if err = s.Get("interactions", "old", &request); err != nil {
		t.Fatal(err)
	}
	if request.State != "expired" || len(x.pendingInteractions("s")) != 0 {
		t.Fatal("callback from old engine can still be answered")
	}
}
