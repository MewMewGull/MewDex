package hub

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/protocol"
)

func TestKindStreamKeepsAccessProjectionAndFilteredCheckpoints(t *testing.T) {
	h, s, server, _ := testHub(t)
	for _, id := range []string{"a", "b", "private"} {
		project := "allowed"
		if id == "private" {
			project = "private"
		}
		if err := s.Put("sessions", id, protocol.Session{ID: id, DeviceID: "node", ProjectID: project}); err != nil {
			t.Fatal(err)
		}
	}
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"allowed"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Pair(code, "phone", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 505; i++ {
		if _, err = h.Ingest(protocol.Event{ID: protocol.ID(), DeviceID: "node", SessionID: "a", Kind: "engine.item/agentMessage/delta", Payload: protocol.JSON("not needed by a notification input")}); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range []string{"a", "b", "private"} {
		for _, kind := range []string{"engine.turn/completed", "interaction.updated"} {
			payload := protocol.JSON(map[string]any{"turn": map[string]string{"id": "turn", "status": "completed"}})
			if kind == "interaction.updated" {
				payload = protocol.JSON(protocol.Interaction{ID: "request-" + session, SessionID: session, State: "pending", Kind: "permissions", CanAccept: true, ExecutionScopeID: "scope", Permissions: &protocol.PermissionGrant{Network: true}})
			}
			if _, err = h.Ingest(protocol.Event{ID: protocol.ID(), DeviceID: "node", SessionID: session, Kind: kind, Payload: payload}); err != nil {
				t.Fatal(err)
			}
		}
	}
	latest, err := s.LatestSequence()
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.URL, token, "POST", "/v1/events/ticket", nil)
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err = json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, strings.Replace(server.URL, "http:", "ws:", 1)+"/v1/events/ws?kind=engine.turn%2Fcompleted&kind=interaction.updated&ticket="+ticket.Ticket, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	completions, interactions, checkpoints := 0, 0, 0
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event protocol.Event
		if err = json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		switch event.Kind {
		case "stream.checkpoint":
			checkpoints++
			if event.Sequence == latest {
				if completions != 2 || interactions != 2 || checkpoints != 2 {
					t.Fatalf("completion=%d interaction=%d checkpoints=%d", completions, interactions, checkpoints)
				}
				return
			}
		case "engine.turn/completed":
			completions++
		case "interaction.updated":
			interactions++
			var interaction protocol.Interaction
			if json.Unmarshal(event.Payload, &interaction) != nil || interaction.CanAccept {
				t.Fatal("notification filter bypassed the identity projection")
			}
		default:
			t.Fatalf("unrequested event kind %q", event.Kind)
		}
		if event.SessionID == "private" {
			t.Fatal("private event leaked through notification filter")
		}
	}
}

func TestEventKindFiltersAreExactAndBounded(t *testing.T) {
	for _, values := range [][]string{{""}, {"*"}, {"white space"}, {"a\n"}, {"汉字"}, {strings.Repeat("a", 129)}, strings.Split(strings.Repeat("a,", 16)+"a", ",")} {
		if _, err := eventKinds(url.Values{"kind": values}); err == nil {
			t.Fatalf("accepted invalid filters %+v", values)
		}
	}
	if kinds, err := eventKinds(url.Values{}); err != nil || kinds != nil {
		t.Fatal("absent filter changed existing streams")
	}
	kinds, err := eventKinds(url.Values{"kind": {"engine.turn/completed", "interaction.updated", "interaction.updated"}})
	if err != nil || len(kinds) != 2 || !kinds["engine.turn/completed"] {
		t.Fatal("valid exact kinds were not preserved")
	}
	_, _, server, admin := testHub(t)
	response := request(t, server.URL, admin, "POST", "/v1/events/ticket", nil)
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err = json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	if response = request(t, server.URL, "", "GET", "/v1/events/ws?kind=*&ticket="+ticket.Ticket, nil); response.StatusCode != 400 {
		t.Fatalf("invalid WebSocket filter status=%d", response.StatusCode)
	}
}

func TestSelectedConversationStreamKeepsNetworkStatusAndCursor(t *testing.T) {
	h, _, server, token := testHub(t)
	for _, id := range []string{"a", "b"} {
		session := protocol.Session{ID: id, DeviceID: "node", ProjectID: "project"}
		if _, err := h.Ingest(protocol.Event{ID: protocol.ID(), SessionID: id, DeviceID: "node", Kind: "session.updated", Payload: protocol.JSON(session)}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.Ingest(protocol.Event{ID: protocol.ID(), SessionID: id, DeviceID: "node", Kind: "message.user", Payload: protocol.JSON(id)}); err != nil {
			t.Fatal(err)
		}
	}
	response := request(t, server.URL, token, "POST", "/v1/events/ticket", map[string]any{})
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, strings.Replace(server.URL, "http:", "ws:", 1)+"/v1/events/ws?sessionId=a&ticket="+ticket.Ticket, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	status, messages, checkpoint := 0, 0, int64(0)
	for checkpoint == 0 {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var event protocol.Event
		if err = json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		switch event.Kind {
		case "session.updated":
			status++
		case "message.user":
			if event.SessionID != "a" {
				t.Fatal("inactive conversation content was streamed")
			}
			messages++
		case "stream.checkpoint":
			checkpoint = event.Sequence
		}
	}
	if status != 2 || messages != 1 || checkpoint != 4 {
		t.Fatalf("status=%d messages=%d checkpoint=%d", status, messages, checkpoint)
	}
}

func TestBootstrapAndHistoryUseProjectScope(t *testing.T) {
	h, s, server, _ := testHub(t)
	if err := h.Device(protocol.Device{ID: "node", Projects: []protocol.Project{{ID: "allowed"}, {ID: "private"}}}); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"allowed", "private"} {
		session := protocol.Session{ID: project, ProjectID: project, DeviceID: "node"}
		if _, err := h.Ingest(protocol.Event{ID: protocol.ID(), SessionID: project, DeviceID: "node", Kind: "session.updated", Payload: protocol.JSON(session)}); err != nil {
			t.Fatal(err)
		}
	}
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"allowed"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Pair(code, "phone", "")
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.URL, token, "GET", "/v1/bootstrap", nil)
	var snapshot protocol.NetworkSnapshot
	if err = json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Cursor != 2 || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ProjectID != "allowed" || len(snapshot.Devices) != 1 || len(snapshot.Devices[0].Projects) != 1 {
		t.Fatalf("scope leaked through bootstrap: %+v", snapshot)
	}
	if response = request(t, server.URL, token, "GET", "/v1/session-events?sessionId=private", nil); response.StatusCode != 404 {
		t.Fatal("private history was readable")
	}
	response = request(t, server.URL, token, "GET", "/v1/session-events?sessionId=allowed&limit=1", nil)
	var page protocol.EventPage
	if err = json.NewDecoder(response.Body).Decode(&page); err != nil || len(page.Events) != 1 || page.Events[0].SessionID != "allowed" {
		t.Fatal("authorized history was not readable")
	}
}
