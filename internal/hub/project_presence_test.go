package hub

import (
	"encoding/json"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func TestProjectReaderSeesBoundExecutorPresenceWithProjectScopedMetadata(t *testing.T) {
	h, state, server, _ := testHub(t)
	h.coordinatorID = "hub"
	h.coordinator = &Coordinator{hub: h, store: state}
	for _, device := range []protocol.Device{
		{ID: "hub", Roles: []string{"coordinator"}},
		{ID: "node", Name: "Execution machine", Roles: []string{"executor"}, Projects: []protocol.Project{{ID: "allowed", Name: "Bound directory", Path: "/work"}, {ID: "private", Name: "Private directory", Path: "/private"}}},
		{ID: "other", Name: "Private machine", Roles: []string{"executor"}},
	} {
		if err := h.Device(device); err != nil {
			t.Fatal(err)
		}
	}
	for _, project := range []protocol.Project{
		{ID: "allowed", Name: "Visible project", Directories: []protocol.ProjectDirectory{{DeviceID: "node", ProjectID: "allowed", Path: "/work"}}},
		{ID: "private", Name: "Private project", Directories: []protocol.ProjectDirectory{{DeviceID: "other", ProjectID: "private", Path: "/private"}}},
	} {
		if err := state.Put("hub_projects", project.ID, project); err != nil {
			t.Fatal(err)
		}
	}
	code, err := state.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "hub", ProjectIDs: []string{"allowed"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := state.Pair(code, "project reader", "")
	if err != nil {
		t.Fatal(err)
	}
	read := func() []protocol.Device {
		t.Helper()
		response := request(t, server.URL, token, "GET", "/v1/devices", nil)
		var devices []protocol.Device
		if err := json.NewDecoder(response.Body).Decode(&devices); err != nil {
			t.Fatal(err)
		}
		return devices
	}
	devices := read()
	if len(devices) != 2 || devices[1].ID != "node" || devices[1].Online || len(devices[1].Projects) != 1 || devices[1].Projects[0].ID != "allowed" {
		t.Fatalf("project presence projection was incomplete or leaked metadata: %+v", devices)
	}
	h.mu.Lock()
	h.peers["node"] = &peer{device: devices[1]}
	h.mu.Unlock()
	if devices = read(); !devices[1].Online {
		t.Fatal("project reader did not see the executor reconnect")
	}
	response := request(t, server.URL, token, "POST", "/v1/commands", protocol.Command{ID: "direct-access", DeviceID: "node", ProjectID: "allowed", Kind: "workspace.browse", Workspace: &protocol.WorkspaceRequest{}})
	if response.StatusCode != 403 {
		t.Fatalf("presence projection granted direct execution-node access: %d", response.StatusCode)
	}
}
