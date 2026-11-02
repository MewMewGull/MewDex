package localnode

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"mewdex/internal/backhaul"
	"mewdex/internal/config"
	"mewdex/internal/hubproxy"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/startup"
)

func TestInstallerStartupChoiceIsDeferredAndDesktopCanDisableIt(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{true: "on", false: "off"}[enabled], func(t *testing.T) {
			m := testManager(t)
			backend := &startupBackendFixture{}
			m.options.StartupBackend = backend
			ctx := context.Background()
			if err := m.SaveStartupPreference(ctx, enabled); err != nil {
				t.Fatal(err)
			}
			if backend.entry.Present {
				t.Fatal("installer registered an unconfigured device")
			}
			status, err := m.Handle(ctx, Request{Operation: "status"})
			if err != nil || status.Snapshot.StartupPreference == nil || *status.Snapshot.StartupPreference != enabled {
				t.Fatal("installer choice was not visible before setup")
			}
			result, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}})
			if err != nil {
				t.Fatal(err)
			}
			if backend.entry.Present != enabled {
				t.Fatal("completed setup did not honor installer choice")
			}
			before, _ := os.ReadFile(m.profile())
			if _, err = m.Handle(ctx, Request{Operation: "startup.apply", ExpectedDeviceID: result.Snapshot.DeviceID, Startup: &StartupRequest{Enabled: false}}); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(m.profile())
			if string(before) != string(after) || backend.entry.Present {
				t.Fatal("desktop startup switch changed the device or left an entry")
			}
			choice, err := m.StartupPreference()
			if err != nil || choice == nil || *choice {
				t.Fatal("desktop change was not saved for installer upgrades")
			}
		})
	}
}

func TestLegacyProxyMigrationRemovesItsOwnEntryAndIsIdempotent(t *testing.T) {
	m := testManager(t)
	backend := &startupBackendFixture{}
	m.options.StartupBackend = backend
	ctx := context.Background()
	setup, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}})
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(m.options.Root, "ingress")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKey(key, "fixture")
	identity := filepath.Join(legacy, "existing key")
	hosts := filepath.Join(legacy, "known hosts")
	program := filepath.Join(legacy, "ssh fixture")
	for path, raw := range map[string][]byte{identity: pem.EncodeToMemory(block), hosts: []byte(""), program: []byte("fixture")} {
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	hash := sha256.Sum256([]byte("fixture"))
	profile := backhaul.Config{ID: "legacy", SSHPath: program, SSHSHA256: hex.EncodeToString(hash[:]), Host: "fixture.invalid", Port: 22, User: "fixture", IdentityFile: identity, KnownHostsFile: hosts, RemoteBind: "127.0.0.1", RemotePort: 47834, LocalPort: 47830, PublicURL: "https://fixture.invalid"}
	if err := os.WriteFile(filepath.Join(legacy, "backhaul.json"), protocol.JSON(profile), 0600); err != nil {
		t.Fatal(err)
	}
	_, binding, err := backhaul.Read(legacy)
	if err != nil {
		t.Fatal(err)
	}
	controller, _ := startup.New(legacy, backend)
	if _, err := controller.Apply(ctx, true, filepath.Join(legacy, "runtimes", "old.exe"), binding); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(m.profile())
	result, err := m.Handle(ctx, Request{Operation: "proxy.import", ExpectedDeviceID: setup.Snapshot.DeviceID})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HubProxy.Settings.Enabled || result.HubProxy.LegacyAvailable || backend.entry.Present {
		t.Fatal("migration left the old automatic entry or lost settings")
	}
	after, _ := os.ReadFile(m.profile())
	if string(before) != string(after) {
		t.Fatal("migration changed the device identity or configuration")
	}
	saved, _, err := hubproxy.Read(m.options.Root)
	if err != nil || saved.IdentityFile != identity || saved.PublicURL != profile.PublicURL {
		t.Fatal("migration changed the existing target or credential references")
	}
	// Retrying an observed migration must not undo a later user's disable.
	saved.Enabled = false
	if err := hubproxy.Save(m.options.Root, saved); err != nil {
		t.Fatal(err)
	}
	result, err = m.Handle(ctx, Request{Operation: "proxy.import", ExpectedDeviceID: setup.Snapshot.DeviceID})
	if err != nil || result.HubProxy.Settings.Enabled {
		t.Fatal("migration replay undid the user's saved switch")
	}
}

func TestProxyStatusRejectsStaleAndForeignInstancesAndPreservesStartupBinding(t *testing.T) {
	m := testManager(t)
	backend := &startupBackendFixture{}
	m.options.StartupBackend = backend
	ctx := context.Background()
	result, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(ctx, Request{Operation: "startup.apply", Startup: &StartupRequest{Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(m.profile())
	settings := hubproxy.Defaults()
	if err = hubproxy.Save(m.options.Root, settings); err != nil {
		t.Fatal(err)
	}
	// Disabled settings can be saved after credentials disappear; they do not
	// rebind or remove the independent OS startup registration.
	if _, err = m.Handle(ctx, Request{Operation: "proxy.apply", ExpectedDeviceID: result.Snapshot.DeviceID, HubProxy: (*HubProxySettings)(&settings)}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(m.profile())
	startup, err := m.Handle(ctx, Request{Operation: "startup.status"})
	if err != nil || !startup.Startup.MatchesProfile || !backend.entry.Present || string(before) != string(after) {
		t.Fatal("proxy save invalidated OS startup")
	}
	if _, err := m.Handle(ctx, Request{Operation: "proxy.apply", HubProxy: (*HubProxySettings)(&settings)}); err == nil {
		t.Fatal("unbound management request was accepted")
	}
	// A manually written profile is used only to exercise observation; enabling
	// through the public manager would validate all credential references.
	settings.Enabled = true
	if err := os.WriteFile(filepath.Join(m.options.Root, "hub-proxy.json"), protocol.JSON(settings), 0600); err != nil {
		t.Fatal(err)
	}
	_, fingerprint, _ := hubproxy.Read(m.options.Root)
	c, _ := config.Read(m.profile())
	if err = os.MkdirAll(c.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	control, err := lifecycle.Start(c.DataDir, c.DeviceID, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.Ready("http://127.0.0.1:47830")
	for _, test := range []struct {
		name, instance, hash, updated string
		connected                     bool
	}{
		{"current", control.InstanceID(), fingerprint, protocol.Now(), true},
		{"foreign-instance", "another", fingerprint, protocol.Now(), false},
		{"foreign-settings", control.InstanceID(), "other", protocol.Now(), false},
		{"stale", control.InstanceID(), fingerprint, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := hubproxy.State{InstanceID: test.instance, Fingerprint: test.hash, State: "connected", UpdatedAt: test.updated}
			raw, _ := json.Marshal(state)
			if err := os.WriteFile(filepath.Join(c.DataDir, "hub-proxy-state.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			status, err := m.proxyStatus(ctx)
			if err != nil || (status.State == "connected") != test.connected {
				t.Fatalf("incorrect observation: %+v %v", status, err)
			}
		})
	}
}
