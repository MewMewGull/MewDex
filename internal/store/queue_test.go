package store

import (
	"testing"

	"mewdex/internal/protocol"
)

func TestQueueProjectionCannotRewriteInputOrReopenCompletedWork(t *testing.T) {
	s := memory(t)
	if err := s.Put("sessions", "s", protocol.Session{ID: "s", DeviceID: "node", ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	item, event, err := s.PutQueueItem(protocol.QueueItem{ID: "q", DeviceID: "node", SessionID: "s", ProjectID: "project", State: "queued", Revision: 1, Input: []protocol.Input{{Type: "text", Text: "reviewed input"}}})
	if err != nil || event.Sequence < 1 || item.Position < 1 {
		t.Fatal(err)
	}
	if unacked, err := s.Unacked(); err != nil || len(unacked) != 1 {
		t.Fatal("queue state has no durable outbox event")
	}
	put := func(value protocol.QueueItem) error {
		_, err := s.Ingest(protocol.Event{ID: protocol.ID(), DeviceID: value.DeviceID, SessionID: value.SessionID, Kind: "queue.item.updated", Payload: protocol.JSON(value)})
		return err
	}
	changed := item
	changed.Input = []protocol.Input{{Type: "text", Text: "unreviewed replacement"}}
	changed.Revision++
	if put(changed) == nil {
		t.Fatal("input was replaced under its existing queue ID")
	}
	original := item
	for _, state := range []string{"dispatching", "running", "completed"} {
		item.State, item.Revision = state, item.Revision+1
		if err := put(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := put(original); err != nil {
		t.Fatal("an older outbox event should be harmless")
	}
	item.State, item.Revision = "queued", item.Revision+1
	if put(item) == nil {
		t.Fatal("completed work was reopened for replay")
	}
	var actual protocol.QueueItem
	if err := s.Get("queue_items", "q", &actual); err != nil || actual.State != "completed" {
		t.Fatal("invalid event changed durable progress")
	}
}
