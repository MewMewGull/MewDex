package localnode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/hubproxy"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
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
