package store

import (
	"context"
	"testing"

	"mewdex/internal/protocol"
)

func TestAttachmentHistoryUsesPositiveDeliveryEvidenceOnly(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	command := func(id, session string) protocol.Command {
		return protocol.Command{ID: id, SessionID: session, Kind: "start", Input: []protocol.Input{{Type: "file", AttachmentID: id}}}
	}
	for _, item := range []struct{ id, session, state string }{{"delivered", "session", "completed"}, {"pending", "session", "accepted"}, {"unknown", "session", "unknown"}, {"failed", "session", "failed"}, {"foreign", "other", "completed"}} {
		if _, _, err = s.Claim(command(item.id, item.session)); err != nil {
			t.Fatal(err)
		}
		if err = s.Finish(protocol.CommandResult{CommandID: item.id, State: item.state}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"event-only", "delivered"} {
		if _, err = s.Append(protocol.Event{ID: id, SessionID: "session", Kind: "message.user", Payload: protocol.JSON(command(id, "session"))}); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []protocol.QueueItem{{ID: "queued", SessionID: "session", Input: command("queued", "session").Input}, {ID: "started", SessionID: "session", TurnID: "confirmed-turn", Input: command("started", "session").Input}} {
		if err = s.Put("queue_items", item.ID, item); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := s.SessionAttachments(context.Background(), "session")
	if err != nil || len(inputs) != 3 || inputs[0].AttachmentID != "delivered" || inputs[1].AttachmentID != "event-only" || inputs[2].AttachmentID != "started" {
		t.Fatalf("history contains unconfirmed, duplicate or foreign inputs: %+v %v", inputs, err)
	}
}
