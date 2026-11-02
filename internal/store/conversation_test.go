package store

import (
	"context"
	"path/filepath"
	"testing"

	"mewdex/internal/protocol"
)

func TestConversationReplyPositionsSurviveReopenAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var last protocol.Event
	for _, item := range []struct{ session, device, kind, itemType, text string }{
		{"a", "windows", "engine.item/completed", "agentMessage", "old reply"},
		{"b", "mac", "engine.item/completed", "agentMessage", "other reply"},
		{"a", "windows", "engine.item/agentMessage/delta", "agentMessage", "fragment"},
		{"a", "windows", "engine.item/completed", "commandExecution", "tool output"},
		{"a", "windows", "engine.item/completed", "agentMessage", ""},
		{"a", "windows", "engine.item/completed", "agentMessage", "new reply"},
	} {
		last, err = s.Append(protocol.Event{ID: protocol.ID(), DeviceID: item.device, SessionID: item.session, Kind: item.kind,
			Payload: protocol.JSON(map[string]any{"item": map[string]string{"id": protocol.ID(), "type": item.itemType, "text": item.text}})})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Append(last); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	updates, err := s.ConversationUpdates(context.Background())
	if err != nil || len(updates) != 2 {
		t.Fatalf("durable reply positions: %+v %v", updates, err)
	}
	positions := map[string]int64{}
	for _, update := range updates {
		positions[update.DeviceID+":"+update.SessionID] = update.Sequence
	}
	if positions["windows:a"] != last.Sequence || positions["mac:b"] != 2 {
		t.Fatalf("positions include tools, fragments or duplicate delivery: %+v", positions)
	}
}
