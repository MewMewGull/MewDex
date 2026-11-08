package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func TestProjectRemovalRequiresAdminAndPreservesSessionScope(t *testing.T) {
	h, state, server, admin := testHub(t)
	h.coordinatorID = "hub"
	h.coordinator = &Coordinator{hub: h, store: state}
	if err := h.Device(protocol.Device{ID: "hub", Roles: []string{"coordinator", "executor"}}); err != nil {
		t.Fatal(err)
	}
	project, err := state.SaveHubProject(h.config.DeviceID, "hp_remove", protocol.HubProjectRequest{Name: "Remove me"})
	if err != nil {
		t.Fatal(err)
	}
	session := protocol.Session{ID: "keep-chat", DeviceID: "hub", ProjectID: project.ID, State: "running"}
	if err = state.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	code, err := state.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "hub", ProjectIDs: []string{project.ID}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client, _, err := state.Pair(code, "reader", "")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/v1/projects/" + project.ID
	for _, check := range []struct {
		token, path string
		status      int
	}{
		{client, endpoint + "?expectedRevision=1", http.StatusForbidden},
		{admin, endpoint, http.StatusBadRequest},
		{admin, endpoint + "?expectedRevision=0", http.StatusConflict},
		{admin, "/v1/projects/hp_missing?expectedRevision=1", http.StatusNotFound},
		{admin, endpoint + "?expectedRevision=1", http.StatusOK},
		{admin, endpoint + "?expectedRevision=1", http.StatusOK},
	} {
		response := request(t, server.URL, check.token, "DELETE", check.path, nil)
		response.Body.Close()
		if response.StatusCode != check.status {
			t.Fatalf("DELETE %s: got %d, want %d", check.path, response.StatusCode, check.status)
		}
	}
	response := request(t, server.URL, admin, "GET", "/v1/projects", nil)
	var projects []protocol.Project
	if err = json.NewDecoder(response.Body).Decode(&projects); err != nil || len(projects) != 0 {
		t.Fatal("removed project is still selectable", projects, err)
	}
	response.Body.Close()
	devices, err := h.visibleDevices(protocol.Identity{Access: protocol.Access{AllDevices: true}})
	if err != nil || len(devices) != 1 || len(devices[0].Projects) != 1 || !devices[0].Projects[0].Removed {
		t.Fatal("conversation execution metadata was lost", devices, err)
	}
	identity, err := state.Authenticate(client)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := h.visibleSessions(identity, "")
	if err != nil || len(sessions) != 1 || sessions[0].ID != session.ID || sessions[0].ProjectID != project.ID || sessions[0].Archived {
		t.Fatal("conversation was lost, moved or archived", sessions, err)
	}
	result := h.coordinator.Handle(context.Background(), protocol.Command{ID: "new-chat", DeviceID: "hub", ProjectID: project.ID, Kind: "create"})
	if result.State != "failed" {
		t.Fatal("removed project allowed a new conversation", result)
	}
	response = request(t, server.URL, admin, "PUT", endpoint, protocol.HubProjectRequest{Name: "stale editor", ExpectedRevision: 2})
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("stale editor restored the project", response.StatusCode)
	}
}
