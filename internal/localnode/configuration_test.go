package localnode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/store"
	"mewdex/internal/workspace"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Root: t.TempDir(), Program: program})
	if err != nil {
		t.Fatal(err)
	}
	fixtureEngine(t, m)
	return m
}

func TestExecutorSetupCanEnterBeforeSelectingProjects(t *testing.T) {
	m := testManager(t)
	fixtureEngine(t, m)
	result, err := m.Handle(context.Background(), Request{Operation: "setup", Setup: &Setup{Hub: true, Executor: true, Name: "empty-workbench", Listen: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Snapshot.Configured || !result.Snapshot.Executor || len(result.Snapshot.Projects) != 0 {
		t.Fatal("setup required or invented a project", result.Snapshot)
	}
	c, err := config.Load(m.profile())
	if err != nil || len(c.Projects) != 0 {
		t.Fatal("service cannot load a project-free configuration", err)
	}
	if _, err = m.Handle(context.Background(), Request{Operation: "projects", ExpectedDeviceID: c.DeviceID, Projects: []protocol.Project{}}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(c.DataDir, "executor.db")); !os.IsNotExist(err) {
		t.Fatal("setup started execution or created execution data")
	}
}

func TestFirstHubSetupCreatesMewDexAccountWithoutSavingPasswordInProfile(t *testing.T) {
	m := testManager(t)
	password := "setup-password-fixture"
	_, err := m.Handle(context.Background(), Request{Operation: "setup", Setup: &Setup{Hub: true, Name: "account-hub", Listen: "127.0.0.1:0", Account: &protocol.MewDexAccountLogin{Username: "owner", Password: password}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{m.profile(), m.draft()} {
		raw, err := os.ReadFile(path)
		if err != nil || bytes.Contains(raw, []byte(password)) {
			t.Fatal("password retained in configuration", path, err)
		}
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, actor, err := s.LoginMewDex("owner", password)
	if err != nil || actor.Role != "admin" || !actor.Access.AllDevices {
		t.Fatal("initial account cannot manage the network", err)
	}
}

// The file is staged, never executed. Network membership failures must retain
// the executor identity even if its invitation has already been consumed.
func fixtureEngine(t *testing.T, m *Manager) {
	t.Helper()
	root := t.TempDir()
	entry := filepath.Join("bin", "codex.exe")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("test engine fixture")
	if err := os.WriteFile(filepath.Join(root, entry), raw, 0700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	manifest := runtimefiles.Manifest{Format: 1, Version: engine.CompatibleVersion, Files: []runtimefiles.File{{Path: filepath.ToSlash(entry), SHA256: hex.EncodeToString(hash[:]), Bytes: int64(len(raw))}}}
	if err := createJSON(filepath.Join(root, "mewdex-engine.json"), manifest); err != nil {
		t.Fatal(err)
	}
	m.options.Engine = filepath.Join(root, entry)
}

func TestRemoteSetupUsesHubWithoutAForwardingGrant(t *testing.T) {
	m := testManager(t)
	fixtureEngine(t, m)
	var pairCalls atomic.Int32
	var grant atomic.Bool
	var pairedDevice atomic.Value
	pairedDevice.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := protocol.Identity{ID: "paired-executor", Role: "executor", DeviceID: pairedDevice.Load().(string), Access: protocol.Access{Gateway: grant.Load()}}
		switch r.URL.Path {
		case "/v1/pair":
			if pairCalls.Add(1) != 1 {
				t.Error("consumed invitation was sent again")
			}
			var request map[string]string
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			if request["code"] != "one-time-test-invitation" {
				t.Error("wrong pairing invitation")
			}
			pairedDevice.Store(request["deviceId"])
			identity.DeviceID = pairedDevice.Load().(string)
			_ = json.NewEncoder(w).Encode(Credentials{Token: "test-device-secret", Identity: identity, ProtocolVersion: protocol.Version})
		case "/v1/me":
			if r.Header.Get("Authorization") != "Bearer test-device-secret" {
				t.Error("stored device credential was not reused")
			}
			_ = json.NewEncoder(w).Encode(identity)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	setup := &Setup{Name: "remote executor", Executor: true, HubURL: server.URL, UseHubGateway: true, Invitation: "one-time-test-invitation", Projects: []protocol.Project{{Path: t.TempDir()}}}
	request := Request{Operation: "setup", Setup: setup}
	if _, err := m.Handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	pending, err := m.Handle(context.Background(), Request{Operation: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !pending.Snapshot.Configured || pending.Snapshot.DeviceID != pairedDevice.Load().(string) || pending.Snapshot.UseHubGateway {
		t.Fatal("execution node retained model routing or lost its identity")
	}
	draft, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(draft, []byte(setup.Invitation)) || bytes.Contains(draft, []byte("test-device-secret")) {
		t.Fatal("invitation or credential leaked into settings")
	}
	setup.Invitation = ""
	grant.Store(true)
	result, err := m.Handle(context.Background(), Request{Operation: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Snapshot.Configured || result.Snapshot.DeviceID != pairedDevice.Load().(string) || pairCalls.Load() != 1 {
		t.Fatal("setup retry created a second executor")
	}
}

func TestRemovedProjectIdentityCannotBeReboundByLocalConfiguration(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	initial, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true, Listen: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	old, err := workspace.CanonicalDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(m.options.Root, "data", "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Put("project_bindings", "removed", map[string]string{"id": "removed", "deviceId": initial.Snapshot.DeviceID, "path": old}); err != nil {
		t.Fatal(err)
	}
	_, err = m.Handle(ctx, Request{Operation: "projects", Projects: []protocol.Project{{ID: "removed", Path: t.TempDir()}}})
	if err == nil {
		t.Fatal("removed project ID was rebound to another directory")
	}
}

func TestSetupPreservesAnExistingProfileAndStoppedProjects(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	setup := &Setup{Hub: true, Name: "network", Listen: "127.0.0.1:0", Projects: []protocol.Project{{ID: "project", Path: t.TempDir()}}}
	result, err := m.Handle(ctx, Request{Operation: "setup", Setup: setup})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.Ready || !result.Snapshot.Configured || result.Snapshot.Executor {
		t.Fatal("setup advertised execution before starting a service")
	}
	before, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(ctx, Request{Operation: "setup", Setup: setup}); err == nil {
		t.Fatal("a second setup overwrote the local device")
	}
	after, _ := os.ReadFile(m.profile())
	if string(before) != string(after) {
		t.Fatal("existing configuration changed")
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(ctx, Request{Operation: "projects", Projects: setup.Projects}); err == nil {
		t.Fatal("project settings changed while service data was in use")
	}
	lock.Close()
	changed := []protocol.Project{{ID: "project", Path: t.TempDir()}}
	if _, err = m.Handle(ctx, Request{Operation: "projects", Projects: changed}); err == nil {
		t.Fatal("existing project ID moved its workspace")
	}
}

func TestUnfinishedSetupReusesItsDeviceAndDoesNotStoreTheInvitation(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	setup := &Setup{Hub: true, Name: "network", Listen: "127.0.0.1:0"}
	result, err := m.Handle(ctx, Request{Operation: "setup", Setup: setup})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(m.profile()); err != nil {
		t.Fatal(err)
	} // Simulated interrupted final profile creation.
	pending, err := m.Handle(ctx, Request{Operation: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !pending.Snapshot.SetupPending {
		t.Fatal("unfinished setup was discarded")
	}
	resumed, err := m.Handle(ctx, Request{Operation: "setup", Setup: setup})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Snapshot.DeviceID != result.Snapshot.DeviceID {
		t.Fatal("setup retry allocated another device identity")
	}
	if _, err = os.Stat(filepath.Join(m.options.Root, "engine-home")); !os.IsNotExist(err) {
		t.Fatal("Hub-only setup touched model account storage")
	}
}
