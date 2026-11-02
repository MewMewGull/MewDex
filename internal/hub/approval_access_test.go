package hub

import (
	"encoding/json"
	"mewdex/internal/protocol"
	"net/http"
	"testing"
	"time"
)

func TestScopedInputCannotGrantExtraPermissions(t *testing.T) {
	_, s, server, admin := testHub(t)
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"project"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client, _, err := s.Pair(code, "phone", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put("sessions", "session", protocol.Session{ID: "session", DeviceID: "node", ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id, kind, scope string
		extra           bool
		clientCan       bool
	}{
		{"permissions", "permissions", "scope", true, false}, {"bounded-command", "command", "scope", false, true}, {"command-extra", "command", "scope", true, false}, {"legacy-command", "command", "", false, false}, {"question", "questions", "", false, true},
	} {
		view := protocol.Interaction{ID: test.id, SessionID: "session", Kind: test.kind, ExecutionScopeID: test.scope, State: "pending", CanAccept: true, Questions: []protocol.InputQuestion{}}
		if test.extra {
			view.Permissions = &protocol.PermissionGrant{Network: true}
		}
		if err = s.Put("interactions", view.ID, view); err != nil {
			t.Fatal(err)
		}
		response := request(t, server.URL, client, "GET", "/v1/interactions?sessionId=session", nil)
		var visible []protocol.Interaction
		if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&visible) != nil {
			t.Fatal("pending request unavailable")
		}
		found := false
		for _, item := range visible {
			if item.ID == view.ID {
				found = true
				if item.CanAccept != test.clientCan {
					t.Fatalf("incorrect approval capability for %s", view.ID)
				}
			}
		}
		if !found {
			t.Fatal("missing request")
		}
		command := protocol.Command{ID: "client-" + view.ID, DeviceID: "node", SessionID: "session", Kind: "respond", RequestID: view.ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})}
		response = request(t, server.URL, client, "POST", "/v1/commands", command)
		if test.clientCan && response.StatusCode != http.StatusAccepted || !test.clientCan && response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s status %d", view.ID, response.StatusCode)
		}
		if test.kind == "permissions" {
			command.ID = "admin-grant"
			response = request(t, server.URL, admin, "POST", "/v1/commands", command)
			if response.StatusCode != http.StatusAccepted {
				t.Fatal("administrator could not review turn grant")
			}
		}
	}
}

func TestApprovalEventProjectionDoesNotChangeTheCanonicalEvent(t *testing.T) {
	original := protocol.Event{ID: "event", Sequence: 42, SessionID: "session", Kind: "interaction.updated", Payload: protocol.JSON(protocol.Interaction{ID: "request", SessionID: "session", Kind: "permissions", ExecutionScopeID: "scope", State: "pending", CanAccept: true, Permissions: &protocol.PermissionGrant{Network: true}})}
	canonical := string(original.Payload)
	for _, role := range []string{"client", "admin"} {
		projected := interactionEventForIdentity(protocol.Identity{Role: role}, original)
		var view protocol.Interaction
		if json.Unmarshal(projected.Payload, &view) != nil || view.CanAccept != (role == "admin") || projected.ID != original.ID || projected.Sequence != original.Sequence {
			t.Fatal("incorrect projected approval")
		}
	}
	if string(original.Payload) != canonical {
		t.Fatal("identity projection rewrote the stored event")
	}
}
