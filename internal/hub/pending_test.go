package hub

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestPendingRecoveryIsSourceAndProjectScoped(t *testing.T) {
	_, s, server, admin := testHub(t)
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"allowed"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, actor, err := s.Pair(code, "phone", "")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := s.Authenticate(admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct{ id, project, source, kind string }{
		{"visible", "allowed", actor.ID, "start"}, {"private", "private", actor.ID, "start"},
		{"another-input", "allowed", owner.ID, "start"}, {"admin-command", "allowed", actor.ID, "account.read"},
	} {
		if err := s.Put("sessions", target.id, protocol.Session{ID: target.id, DeviceID: "node", ProjectID: target.project}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Claim(protocol.Command{ID: target.id, DeviceID: "node", SessionID: target.id, ProjectID: target.project, SourceID: target.source, Kind: target.kind}); err != nil {
			t.Fatal(err)
		}
	}
	response := request(t, server.URL, token, "GET", "/v1/pending-commands?deviceId=node&sourceId="+owner.ID, nil)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	var page protocol.PendingCommandPage
	if err = json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Commands) != 1 || page.Commands[0].ID != "visible" {
		t.Fatalf("unscoped recovery: %+v", page)
	}
	response = request(t, server.URL, admin, "GET", "/v1/pending-commands?deviceId=node", nil)
	if err = json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Commands) != 1 || page.Commands[0].ID != "another-input" {
		t.Fatal("admin recovered another input identity's pending commands")
	}
	if response = request(t, server.URL, token, "GET", "/v1/pending-commands?deviceId=another-node", nil); response.StatusCode != 404 {
		t.Fatal("ungranted device was visible")
	}
	if response = request(t, server.URL, token, "GET", "/v1/pending-commands?deviceId=node&after=-1", nil); response.StatusCode != 400 {
		t.Fatal("invalid cursor was accepted")
	}
}

func TestInputCanRecoverUnknownReferenceAfterPersistentHubRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.Invite("client", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, actor, err := s.Pair(code, "persistent input", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Claim(protocol.Command{ID: "lost-input-local-state", SourceID: actor.ID, DeviceID: "node", ProjectID: "project", Kind: "start"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err = reopened.RecoverCommands(""); err != nil {
		t.Fatal(err)
	}
	h := New(config.Config{DeviceID: "hub"}, reopened)
	server := httptest.NewServer(h.Handler())
	defer server.Close()
	// The restarted input knows only its saved credential and selected target.
	response := request(t, server.URL, token, "GET", "/v1/pending-commands?deviceId=node", nil)
	var page protocol.PendingCommandPage
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&page) != nil {
		t.Fatal("persistent input could not query its references")
	}
	if len(page.Commands) != 1 || page.Commands[0].ID != "lost-input-local-state" || page.Commands[0].State != "unknown" {
		t.Fatalf("restart recovery: %+v", page)
	}
}
