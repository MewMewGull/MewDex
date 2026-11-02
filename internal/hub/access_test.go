package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func testHub(t *testing.T) (*Hub, *store.Store, *httptest.Server, string) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := New(config.Config{DeviceID: "local"}, s)
	server := httptest.NewServer(h.Handler())
	t.Cleanup(server.Close)
	code, err := s.Invite("admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Pair(code, "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	return h, s, server, token
}

func request(t *testing.T, base, token, method, path string, body any) *http.Response {
	t.Helper()
	var data []byte
	if body != nil {
		data = protocol.JSON(body)
	}
	r, err := http.NewRequest(method, base+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestQueueQueryRespectsSessionProjectScope(t *testing.T) {
	_, s, server, admin := testHub(t)
	for _, project := range []string{"allowed", "private"} {
		if err := s.Put("sessions", project, protocol.Session{ID: project, DeviceID: "node", ProjectID: project}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.PutQueueItem(protocol.QueueItem{ID: "q-" + project, DeviceID: "node", SessionID: project, ProjectID: project, State: "queued", Revision: 1, Input: []protocol.Input{{Type: "text", Text: project}}}); err != nil {
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
	response := request(t, server.URL, token, "GET", "/v1/queue?sessionId=private", nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("private queue was disclosed")
	}
	response = request(t, server.URL, token, "GET", "/v1/queue?sessionId=allowed", nil)
	var items []protocol.QueueItem
	if err = json.NewDecoder(response.Body).Decode(&items); err != nil || len(items) != 1 || items[0].SessionID != "allowed" {
		t.Fatalf("allowed queue unavailable: %+v %v", items, err)
	}
	if response = request(t, server.URL, admin, "GET", "/v1/queue?sessionId=private", nil); response.StatusCode != http.StatusOK {
		t.Fatal("admin could not read authorized queue")
	}
}

func TestScopeFiltersQueriesAndRejectsCommands(t *testing.T) {
	h, s, server, _ := testHub(t)
	device := protocol.Device{ID: "node", Projects: []protocol.Project{{ID: "allowed", Path: "/allowed"}, {ID: "private", Path: "/private"}}}
	if err := h.Device(device); err != nil {
		t.Fatal(err)
	}
	for _, project := range device.Projects {
		session := protocol.Session{ID: project.ID, DeviceID: device.ID, ProjectID: project.ID}
		if _, err := h.Ingest(protocol.Event{ID: project.ID, DeviceID: device.ID, SessionID: session.ID, Kind: "session.updated", Payload: protocol.JSON(session)}); err != nil {
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
	response := request(t, server.URL, token, "GET", "/v1/devices", nil)
	var devices []protocol.Device
	if err = json.NewDecoder(response.Body).Decode(&devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || len(devices[0].Projects) != 1 || devices[0].Projects[0].ID != "allowed" {
		t.Fatalf("project names leaked: %+v", devices)
	}
	response = request(t, server.URL, token, "GET", "/v1/sessions", nil)
	var sessions []protocol.Session
	if err = json.NewDecoder(response.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "allowed" {
		t.Fatalf("sessions leaked: %+v", sessions)
	}
	response = request(t, server.URL, token, "GET", "/v1/events", nil)
	var events []protocol.Event
	if err = json.NewDecoder(response.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].SessionID != "allowed" {
		t.Fatalf("events leaked: %+v", events)
	}
	response = request(t, server.URL, token, "POST", "/v1/commands", protocol.Command{ID: "forbidden", DeviceID: "node", SessionID: "private", Kind: "start"})
	if response.StatusCode != 403 {
		t.Fatalf("private command status=%d", response.StatusCode)
	}
	response = request(t, server.URL, token, "GET", "/v1/identities", nil)
	if response.StatusCode != 403 {
		t.Fatal("client accessed identity administration")
	}
	response = request(t, server.URL, token, "POST", "/v1/commands", protocol.Command{ID: "login", DeviceID: "node", Kind: "account.login"})
	if response.StatusCode != 403 {
		t.Fatal("non-admin input could change model login")
	}
	for _, kind := range []string{"account.read", "account.limits", "account.logout", "workspace.browse", "workspace.create", "workspace.update"} {
		response = request(t, server.URL, token, "POST", "/v1/commands", protocol.Command{ID: kind, DeviceID: "node", Kind: kind})
		if response.StatusCode != 403 {
			t.Fatalf("non-admin accessed %s: %d", kind, response.StatusCode)
		}
		response.Body.Close()
	}
}

func TestRevocationClosesExistingEventStream(t *testing.T) {
	_, s, server, admin := testHub(t)
	code, err := s.Invite("client", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, id, err := s.Pair(code, "phone", "")
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.URL, token, "POST", "/v1/events/ticket", map[string]any{})
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	if err = json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, strings.Replace(server.URL, "http:", "ws:", 1)+"/v1/events/ws?ticket="+ticket.Ticket, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	response = request(t, server.URL, admin, "DELETE", "/v1/identities/"+id.ID, nil)
	if response.StatusCode != 200 {
		t.Fatal("revoke failed")
	}
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("revoked event stream remained usable")
	}
	if ctx.Err() != nil {
		t.Fatal("revocation waited for read timeout")
	}
}

func TestGatewayGrantUsesExecutorIdentityAndScope(t *testing.T) {
	h, s, _, _ := testHub(t)
	code, err := s.InviteWithAccess("executor", protocol.Access{Gateway: true}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, id, err := s.Pair(code, "server", "server-id")
	if err != nil {
		t.Fatal(err)
	}
	grant, err := h.AuthorizeGateway(context.Background(), "server-id", token)
	if err != nil || grant.IdentityID != id.ID {
		t.Fatalf("gateway grant: %+v %v", grant, err)
	}
	if _, err = h.AuthorizeGateway(context.Background(), "other-server", token); err == nil {
		t.Fatal("another device reused gateway identity")
	}
	if err = s.UpdateAccess(id.ID, protocol.Access{}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.AuthorizeGateway(context.Background(), "server-id", token); err == nil {
		t.Fatal("gateway scope removal was not applied")
	}
}

func TestPromptHistoryRespectsProjectScope(t *testing.T) {
	h, s, server, _ := testHub(t)
	for _, id := range []string{"allowed", "private"} {
		if err := s.Put("sessions", id, protocol.Session{ID: id, DeviceID: "node", ProjectID: id}); err != nil {
			t.Fatal(err)
		}
		value := protocol.PolicySnapshot{ID: id + ":v", SessionID: id, Version: "v", DeveloperInstructions: id, CreatedAt: "now"}
		if _, err := h.Ingest(protocol.Event{ID: "snapshot-" + id, SessionID: id, DeviceID: "node", Kind: "policy.snapshot", Payload: protocol.JSON(value)}); err != nil {
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
	if response := request(t, server.URL, token, "GET", "/v1/policies?sessionId=private", nil); response.StatusCode != 404 {
		t.Fatal("private prompt history was exposed")
	}
	response := request(t, server.URL, token, "GET", "/v1/policies?sessionId=allowed", nil)
	var values []protocol.PolicySnapshot
	if err = json.NewDecoder(response.Body).Decode(&values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].DeveloperInstructions != "allowed" {
		t.Fatalf("wrong prompt history: %+v", values)
	}
}
