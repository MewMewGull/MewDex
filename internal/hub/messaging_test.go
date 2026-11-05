package hub

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"mewdex/internal/inputbridge"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"net/http/httptest"
)

func TestMessagingSettingsRequireAdminAndNeverReturnSecrets(t *testing.T) {
	h, s, server, admin := testHub(t)
	startAPPBridge(t, h, s, server, admin)
	secret := "private-channel-credential"
	response := request(t, server.URL, admin, "PUT", "/v1/messaging/qq", protocol.MessagingUpdate{AppID: "qq-app", Secret: &secret})
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || strings.Contains(string(data), secret) || !strings.Contains(string(data), `"credentialConfigured":true`) {
		t.Fatalf("unsafe settings response: %d %s", response.StatusCode, data)
	}
	code, _ := s.Invite("client", time.Minute)
	client, _, _ := s.Pair(code, "external-input", "")
	for _, call := range []struct{ method, path string }{
		{"GET", "/v1/messaging"}, {"PUT", "/v1/messaging/qq"}, {"POST", "/v1/messaging/weixin/login"}, {"POST", "/v1/messaging/weixin/login/poll"},
	} {
		response = request(t, server.URL, client, call.method, call.path, protocol.MessagingUpdate{})
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("ordinary input can administer channels: %s %s: %d", call.method, call.path, response.StatusCode)
		}
	}
	response = request(t, server.URL, admin, "GET", "/v1/messaging", nil)
	var channels []protocol.MessagingChannel
	if json.NewDecoder(response.Body).Decode(&channels) != nil || len(channels) != 3 {
		t.Fatal("three channel views were not exposed")
	}
	response.Body.Close()
}

func TestMessagingCommandsUseCurrentScopedIdentityAndApprovalChecks(t *testing.T) {
	h, s, _, _ := testHub(t)
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "local", ProjectIDs: []string{"project"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, id, err := s.Pair(code, "消息渠道 / test", "")
	if err != nil {
		t.Fatal(err)
	}
	command := protocol.Command{ID: "wrong-project", DeviceID: "local", ProjectID: "other", Kind: "create"}
	if _, err = h.submitMessagingCommand(context.Background(), id.ID, command); err == nil {
		t.Fatal("messaging command escaped the configured project")
	}
	if err = s.Put("sessions", "session", protocol.Session{ID: "session", DeviceID: "local", ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	request := protocol.Interaction{ID: "permission", SessionID: "session", Kind: "permissions", State: "pending", CanAccept: true, ExecutionScopeID: "scope", Permissions: &protocol.PermissionGrant{Network: true}}
	if err = s.Put("interactions", request.ID, request); err != nil {
		t.Fatal(err)
	}
	command = protocol.Command{ID: "extra-permission", DeviceID: "local", SessionID: "session", Kind: "respond", RequestID: request.ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})}
	if _, err = h.submitMessagingCommand(context.Background(), id.ID, command); err == nil {
		t.Fatal("channel identity granted administrator-only permissions")
	}
	command = protocol.Command{ID: "allowed-but-offline", DeviceID: "local", ProjectID: "project", Kind: "create", SourceID: "spoofed-admin"}
	if _, err = h.submitMessagingCommand(context.Background(), id.ID, command); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Command(command.ID)
	if err != nil || stored.SourceID != id.ID {
		t.Fatal("command source was not bound to its scoped channel identity")
	}
	if err = s.Revoke(id.ID); err != nil {
		t.Fatal(err)
	}
	command.ID = "after-revocation"
	if _, err = h.submitMessagingCommand(context.Background(), id.ID, command); err == nil {
		t.Fatal("revoked identity kept submitting messages")
	}
}

func TestMessagingSettingsRequireHubProjectInCoordinatorMode(t *testing.T) {
	h, s, server, admin := testHub(t)
	h.coordinatorID = "local.hub"
	h.coordinator = &Coordinator{hub: h}
	startAPPBridge(t, h, s, server, admin)
	response := request(t, server.URL, admin, "PUT", "/v1/messaging/qq", protocol.MessagingUpdate{Enabled: true, DeviceID: "local", ProjectID: "project"})
	defer response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal("a channel was configured to bypass Hub project routing", response.Status)
	}
}

func startAPPBridge(t *testing.T, h *Hub, s *store.Store, server *httptest.Server, token string) {
	t.Helper()
	identity, err := s.Authenticate(token)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	root := t.TempDir()
	go func() {
		done <- inputbridge.Run(ctx, inputbridge.Options{Root: root, BaseURL: server.URL, Token: token, HubDeviceID: h.config.DeviceID, IdentityID: identity.ID})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("APP bridge did not stop")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		connected := len(h.inputBridges) > 0
		h.mu.Unlock()
		if connected {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("APP bridge exited: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("APP bridge did not connect")
}
