package inputbridge

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/hub"
	"mewdex/internal/messaging"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestAPPChannelUsesRemoteProjectAndScopedCommandIdentity(t *testing.T) {
	central, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { central.Close() })
	h := hub.New(config.Config{DeviceID: "hub"}, central)
	if err = h.Device(protocol.Device{ID: "executor", Roles: []string{"executor"}, Projects: []protocol.Project{{ID: "project", Name: "Project"}}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h.Handler())
	t.Cleanup(server.Close)
	code, err := central.Invite("admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := central.Pair(code, "APP", "")
	if err != nil {
		t.Fatal(err)
	}
	local, err := store.Open(filepath.Join(t.TempDir(), "channels.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { local.Close() })
	b := newBackend(context.Background(), server.URL, token, local)
	manager, err := messaging.NewWithBackend(local, b, b.Submit)
	if err != nil {
		t.Fatal(err)
	}
	secret := "owned-platform-secret"
	if err = manager.Update("qq", protocol.MessagingUpdate{Enabled: true, DeviceID: "executor", ProjectID: "project", AppID: "app", Secret: &secret, AllowedUsers: []string{"user"}}); err != nil {
		t.Fatal(err)
	}
	var saved struct {
		IdentityID string `json:"identityId"`
	}
	if err = local.Get("messaging_channels", "qq", &saved); err != nil {
		t.Fatal(err)
	}
	identity, err := central.Identity(saved.IdentityID)
	if err != nil || identity.Role != "client" || !identity.Access.Allows("executor", "project") || identity.Access.Allows("executor", "other") {
		t.Fatal("APP channel identity escaped its project", err)
	}
	command := protocol.Command{ID: "app-channel-command", DeviceID: "executor", ProjectID: "project", Kind: "create"}
	if _, err = b.Submit(context.Background(), identity.ID, command); err != nil {
		t.Fatal(err)
	}
	stored, err := central.Command(command.ID)
	if err != nil || stored.SourceID != identity.ID {
		t.Fatal("APP channel command did not enter Hub as its scoped input", err)
	}
	command.ID, command.ProjectID = "outside-channel-project", "other"
	if _, err = b.Submit(context.Background(), identity.ID, command); err == nil {
		t.Fatal("APP channel submitted outside its selected project")
	}
	if err = manager.Update("qq", protocol.MessagingUpdate{Enabled: true, DeviceID: "executor", AppID: "app", AllowedUsers: []string{"user"}}); err != nil {
		t.Fatal("APP channel could not enable chat-based project selection", err)
	}
	identity, err = central.Identity(saved.IdentityID)
	if err != nil || !identity.Access.Allows("executor", "other") || identity.Access.Allows("unrelated-device", "other") || identity.Access.AllDevices || identity.Access.Gateway {
		t.Fatal("selectable channel access escaped its Hub target", identity, err)
	}
	if err = h.Device(protocol.Device{ID: "executor", Roles: []string{"executor"}, Projects: []protocol.Project{{ID: "project", Name: "Project"}, {ID: "other", Name: "New Project"}}}); err != nil {
		t.Fatal(err)
	}
	var catalogue protocol.Device
	if err = b.Get("devices", "executor", &catalogue); err != nil || len(catalogue.Projects) != 2 {
		t.Fatal("APP channel did not see the current remote project catalogue", catalogue, err)
	}
	command.ID = "newly-selected-project"
	if _, err = b.Submit(context.Background(), identity.ID, command); err != nil {
		t.Fatal("APP channel could not submit to another available project", err)
	}
	if err = central.Revoke(identity.ID); err != nil {
		t.Fatal(err)
	}
	command.ID, command.ProjectID = "revoked-channel-command", "project"
	if _, err = b.Submit(context.Background(), identity.ID, command); err == nil {
		t.Fatal("revoked APP channel kept submitting commands")
	}
}
