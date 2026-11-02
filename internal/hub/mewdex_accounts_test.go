package hub

import (
	"encoding/json"
	"mewdex/internal/protocol"
	"net/http"
	"testing"
)

func TestMewDexNegotiationLoginAndAllExecutorManagement(t *testing.T) {
	h, _, server, key := testHub(t)
	info := request(t, server.URL, "", "GET", "/health", nil)
	var health protocol.ServerInfo
	if err := json.NewDecoder(info.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Authentication.DefaultMethod != "key" || health.Authentication.AccountConfigured {
		t.Fatal("legacy Hub should advertise key bootstrap", health)
	}
	body := map[string]string{"username": "owner", "name": "中央账号", "password": "password-fixture"}
	if r := request(t, server.URL, "", "PUT", "/v1/mewdex/account", body); r.StatusCode != http.StatusUnauthorized {
		t.Fatal("anonymous account creation allowed")
	}
	if r := request(t, server.URL, key, "PUT", "/v1/mewdex/account", body); r.StatusCode != 200 {
		t.Fatal("account setup failed", r.Status)
	}
	info = request(t, server.URL, "", "GET", "/health", nil)
	if err := json.NewDecoder(info.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Authentication.DefaultMethod != "password" || len(health.Authentication.Methods) != 2 {
		t.Fatal("server did not advertise password and key", health)
	}
	r := request(t, server.URL, "", "POST", "/v1/login", map[string]string{"username": "owner", "password": "wrong"})
	if r.StatusCode != 401 {
		t.Fatal("invalid password accepted")
	}
	r = request(t, server.URL, "", "POST", "/v1/login", body)
	var credentials struct {
		Token    string            `json:"token"`
		Identity protocol.Identity `json:"identity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil || r.StatusCode != 200 {
		t.Fatal("password login failed", err)
	}
	for _, device := range []string{"local", "remote-new"} {
		if err := h.Device(protocol.Device{ID: device, Projects: []protocol.Project{{ID: "project"}}}); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"create", "account.read", "account.login", "account.logout", "workspace.create", "workspace.update"} {
			if !h.allowsCommand(credentials.Identity, protocol.Command{DeviceID: device, ProjectID: "project", Kind: kind}) {
				t.Fatalf("MewDex account cannot manage %s on %s", kind, device)
			}
		}
	}
	r = request(t, server.URL, credentials.Token, "GET", "/v1/bootstrap", nil)
	var snapshot protocol.NetworkSnapshot
	if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil || len(snapshot.Devices) != 2 {
		t.Fatal("new terminal needs separate device grants", err)
	}
	r = request(t, server.URL, key, "GET", "/v1/me", nil)
	var identity protocol.Identity
	if err := json.NewDecoder(r.Body).Decode(&identity); err != nil || identity.ID != credentials.Identity.ID {
		t.Fatal("existing key no longer shares account", err)
	}
	for i := 0; i < 10; i++ {
		r = request(t, server.URL, "", "POST", "/v1/login", body)
	}
	if r.StatusCode != 429 {
		t.Fatal("login attempts are not bounded")
	}
}

func TestCentralProjectRenameWorksOfflineAndSurvivesStaleNodeReports(t *testing.T) {
	h, s, server, key := testHub(t)
	device := protocol.Device{ID: "remote", Projects: []protocol.Project{{ID: "p", Name: "old", Path: "/projects/p"}}}
	if err := h.Device(device); err != nil {
		t.Fatal(err)
	}
	command := protocol.Command{ID: "rename", DeviceID: device.ID, ProjectID: "p", Kind: "workspace.update", Workspace: &protocol.WorkspaceRequest{Path: "/cannot-rebind", Name: "中央名称", Category: "中央分类"}}
	r := request(t, server.URL, key, "POST", "/v1/commands", command)
	var result protocol.CommandResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil || result.State != "completed" {
		t.Fatal("offline rename failed", result, err)
	}
	if err := h.Device(device); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Ingest(protocol.Event{ID: "stale-node-catalog", DeviceID: device.ID, Kind: "workspace.catalog.updated", Payload: protocol.JSON(device.Projects), CreatedAt: protocol.Now()}); err != nil {
		t.Fatal(err)
	}
	var current protocol.Device
	if err := s.Get("devices", device.ID, &current); err != nil {
		t.Fatal(err)
	}
	project := current.Projects[0]
	if project.Name != "中央名称" || project.Category != "中央分类" || project.Path != "/projects/p" {
		t.Fatal("node overwrote central metadata or path changed", project)
	}
	r = request(t, server.URL, key, "POST", "/v1/commands", command)
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil || result.State != "completed" {
		t.Fatal("rename receipt not idempotent", err)
	}
	if len(h.peers) != 0 {
		t.Fatal("offline metadata update connected a node")
	}
}
