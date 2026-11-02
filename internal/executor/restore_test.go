package executor

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestRestoredExecutorRepublishesExpiredCallbacksWithoutReplayingInputs(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Put("sessions", "session", protocol.Session{ID: "session", DeviceID: "device", ProjectID: "project", State: "running", EngineID: "thread", EngineStarted: true, TurnID: "old-turn"}); err != nil {
		t.Fatal(err)
	}
	request := protocol.Interaction{ID: "request", SessionID: "session", TurnID: "old-turn", State: "pending", Kind: "command"}
	if err = s.Put("interactions", request.ID, request); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Append(protocol.Event{ID: "old-event", DeviceID: "device", SessionID: "session", Kind: "interaction.updated", Payload: protocol.JSON(request)}); err != nil {
		t.Fatal(err)
	}
	if err = s.PrepareRestore(context.Background(), "device", true); err != nil {
		t.Fatal(err)
	}
	fake := &fixtureEngine{notifications: make(chan engine.Notification, 10)}
	x, err := New(config.Config{DeviceID: "device", DataDir: dir, Projects: []protocol.Project{{ID: "project", Path: dir}}}, s, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	events, err := s.Unacked()
	if err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	for _, event := range events {
		if event.Kind == "interaction.updated" {
			var actual protocol.Interaction
			if err = json.Unmarshal(event.Payload, &actual); err != nil || actual.State != "expired" || actual.ID != request.ID {
				t.Fatal("restored callback could still be answered")
			}
			callbacks++
		}
	}
	if callbacks != 1 {
		t.Fatal("expired callback projection was not rebuilt exactly once")
	}
	var marker map[string]bool
	if err = s.Get("maintenance", "restore_pending", &marker); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("restore rebuild marker remained active")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, method := range fake.methods {
		if method == "turn/start" || method == "turn/steer" {
			t.Fatal("restored input was submitted again")
		}
	}
}
