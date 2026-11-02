package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/protocol"
)

func TestConversationReplySnapshotAndScopedStream(t *testing.T) {
	h, s, server, _ := testHub(t)
	for _, id := range []string{"selected", "background", "private"} {
		project := "work"
		if id == "private" {
			project = "private"
		}
		if err := s.Put("sessions", id, protocol.Session{ID: id, DeviceID: "node", ProjectID: project}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Ingest(protocol.Event{ID: protocol.ID(), DeviceID: "node", SessionID: id, Kind: "engine.item/completed",
			Payload: protocol.JSON(map[string]any{"item": map[string]string{"id": "reply", "type": "agentMessage", "text": "private reply content"}})}); err != nil {
			t.Fatal(err)
		}
	}
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"work"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Pair(code, "input", "")
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.URL, token, "GET", "/v1/bootstrap", nil)
	var snapshot protocol.NetworkSnapshot
	if err = json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ConversationUpdates) != 2 || len(snapshot.Sessions) != 2 {
		t.Fatalf("reply positions exceeded project access: %+v", snapshot)
	}
	for _, update := range snapshot.ConversationUpdates {
		if update.SessionID == "private" || update.DeviceID != "node" || update.Sequence == 0 {
			t.Fatalf("unexpected reply position: %+v", update)
		}
	}
	response = request(t, server.URL, token, "POST", "/v1/events/ticket", nil)
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err = json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, strings.Replace(server.URL, "http:", "ws:", 1)+"/v1/events/ws?sessionId=selected&ticket="+ticket.Ticket, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	selected, hints := 0, 0
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event protocol.Event
		if err = json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		if event.Kind == "stream.checkpoint" {
			if selected != 1 || hints != 1 || event.Sequence != snapshot.Cursor {
				t.Fatalf("selected=%d hints=%d cursor=%d", selected, hints, event.Sequence)
			}
			break
		}
		if event.SessionID == "private" {
			t.Fatal("inaccessible conversation was streamed")
		}
		if event.Kind == "conversation.changed" {
			hints++
			var update protocol.ConversationUpdate
			if json.Unmarshal(event.Payload, &update) != nil || update.Sequence != event.Sequence || update.SessionID != "background" || bytes.Contains(data, []byte("private reply content")) {
				t.Fatalf("background hint contained content or lost its position: %s", data)
			}
		} else if event.Kind == "engine.item/completed" && event.SessionID == "selected" {
			selected++
		} else {
			t.Fatalf("unexpected event: %s", data)
		}
	}
}
