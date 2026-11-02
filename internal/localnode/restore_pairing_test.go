package localnode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

func TestRestoredExecutorMustPairAgainAndKeepsItsDeviceID(t *testing.T) {
	m := testManager(t)
	fixtureEngine(t, m)
	ctx := context.Background()
	var device atomic.Value
	device.Store("")
	var pairs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := protocol.Identity{ID: "executor-identity", Role: "executor", DeviceID: device.Load().(string), Access: protocol.Access{Gateway: true}}
		switch r.URL.Path {
		case "/v1/pair":
			var request map[string]string
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			device.Store(request["deviceId"])
			identity.DeviceID = request["deviceId"]
			token := "original-executor-token"
			if pairs.Add(1) > 1 {
				token = "replacement-executor-token"
			}
			_ = json.NewEncoder(w).Encode(Credentials{Token: token, Identity: identity, ProtocolVersion: protocol.Version})
		case "/v1/me":
			_ = json.NewEncoder(w).Encode(identity)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if _, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Name: "executor", Executor: true, HubURL: server.URL, UseHubGateway: true, Invitation: "original-invitation", Projects: []protocol.Project{{Path: t.TempDir()}}}}); err != nil {
		t.Fatal(err)
	}
	before, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "executor.age")
	if _, err = m.Handle(ctx, Request{Operation: "backup", Backup: &BackupRequest{Path: archive, Passphrase: restorePassword}}); err != nil {
		t.Fatal(err)
	}
	plan := prepareTestRestore(t, m, archive)
	applied, err := m.Handle(ctx, Request{Operation: "restore.apply", Restore: &RestoreRequest{ID: plan.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Snapshot.PairingRequired {
		t.Fatal("restored executor reused snapshot access")
	}
	if _, err = m.Handle(ctx, Request{Operation: "start"}); err == nil || !strings.Contains(err.Error(), "pair") {
		t.Fatal("restored executor could start without repairing its identity")
	}
	if _, err = m.Handle(ctx, Request{Operation: "repair.executor", ExpectedDeviceID: before.DeviceID, Pairing: &PairingRequest{Invitation: "new-invitation"}}); err != nil {
		t.Fatal(err)
	}
	after, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(after.NodeTokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if after.DeviceID != before.DeviceID || string(token) != "replacement-executor-token" || pairs.Load() != 2 {
		t.Fatal("repair reused an old token or created another device")
	}
	original, err := os.ReadFile(before.NodeTokenFile)
	if err != nil || string(original) != "original-executor-token" {
		t.Fatal("repair modified the preserved previous identity file")
	}
	if _, err = m.Handle(ctx, Request{Operation: "repair.executor", Pairing: &PairingRequest{}}); err != nil || pairs.Load() != 2 {
		t.Fatal("repair retry issued another pairing request")
	}
}
