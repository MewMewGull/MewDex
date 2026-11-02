package store

import (
	"testing"

	"mewdex/internal/protocol"
)

func TestStartupDoesNotLeaveAnUnconfirmedDeliveryAcceptedForever(t *testing.T) {
	s := memory(t)
	commands := []protocol.Command{
		{ID: "uncertain", DeviceID: "node", SourceID: "phone", SessionID: "s", Kind: "start"},
		{ID: "other", DeviceID: "other-node", SourceID: "phone", SessionID: "other-s", Kind: "start"},
	}
	for _, command := range commands {
		if _, _, err := s.Claim(command); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put("sessions", "s", protocol.Session{ID: "s", DeviceID: "node", LastStartID: "uncertain", EngineStarted: true, State: "unknown"}); err != nil {
		t.Fatal(err)
	}
	results, err := s.RecoverCommands("node")
	if err != nil || len(results) != 1 || results[0].State != "unknown" {
		t.Fatalf("%+v %v", results, err)
	}
	if r, _ := s.Result("other"); r.State != "accepted" {
		t.Fatal("executor changed another device's delivery")
	}
	if r, owner, err := s.Claim(commands[0]); err != nil || owner || r.State != "unknown" {
		t.Fatal("startup allowed an uncertain command to execute again")
	}
}

func TestStartupRecoversReceiptsOnlyFromDurableCommandEvidence(t *testing.T) {
	s := memory(t)
	create := protocol.Command{ID: "create", DeviceID: "node", ProjectID: "p", Kind: "create", SourceID: "phone"}
	start := protocol.Command{ID: "start", DeviceID: "node", SessionID: "s_create", Kind: "start", SourceID: "phone"}
	enqueue := protocol.Command{ID: "queued", DeviceID: "node", SessionID: "s_create", Kind: "enqueue", SourceID: "phone"}
	for _, command := range []protocol.Command{create, start, enqueue} {
		if _, _, err := s.Claim(command); err != nil {
			t.Fatal(err)
		}
	}
	session := protocol.Session{ID: "s_create", DeviceID: "node", ProjectID: "p", LastStartID: "start", EngineStarted: true, State: "running"}
	if err := s.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(protocol.Event{ID: protocol.ID(), DeviceID: "node", SessionID: session.ID, Kind: "message.user", Payload: protocol.JSON(start)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PutQueueItem(protocol.QueueItem{ID: "queued", SessionID: session.ID, DeviceID: "node", ProjectID: "p", State: "queued", Revision: 1, Input: []protocol.Input{{Type: "text", Text: "waiting"}}}); err != nil {
		t.Fatal(err)
	}
	results, err := s.RecoverCommands("node")
	if err != nil || len(results) != 3 {
		t.Fatalf("%+v %v", results, err)
	}
	for _, result := range results {
		if result.State != "completed" || result.Session == nil {
			t.Fatalf("durable receipt not recovered: %+v", result)
		}
	}
	if again, err := s.RecoverCommands("node"); err != nil || len(again) != 0 {
		t.Fatal("recovered receipts were processed twice")
	}
}
