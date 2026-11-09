package localnode

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
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

func TestRunningHubIsIdentifiedAfterItsProfileBecomesExecutionOnly(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	if _, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(c.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	control, err := lifecycle.Start(c.DataDir, c.DeviceID, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.Ready("http://127.0.0.1:47830")
	c.Hub, c.Executor = false, true
	c.HubURL = "https://hub.example.com"
	if err = replaceJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	status, err := m.Handle(ctx, Request{Operation: "status"})
	if err != nil || !status.Snapshot.Hub || !status.Snapshot.Ready {
		t.Fatal("the running Hub was hidden by its changed profile", status, err)
	}
}

func TestLegacyProxySettingsAreIgnoredAndOperationsAreRemoved(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	setup, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}})
	if err != nil {
		t.Fatal(err)
	}
	profileBefore, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hub-proxy.json", "ingress/backhaul.json"} {
		path := filepath.Join(m.options.Root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"enabled":true,"host":"retired.invalid"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	status, err := m.Handle(ctx, Request{Operation: "status"})
	if err != nil || !status.Snapshot.Configured || !status.Snapshot.Hub {
		t.Fatalf("legacy proxy settings affected Hub status: %#v / %v", status, err)
	}
	for _, operation := range []string{"proxy.status", "proxy.apply", "proxy.import", "proxy.ssh.config", "proxy.test"} {
		if slices.Contains(status.Snapshot.Operations, operation) {
			t.Fatalf("removed operation is still advertised: %s", operation)
		}
		_, err := m.Handle(ctx, Request{Operation: operation, ExpectedDeviceID: setup.Snapshot.DeviceID})
		if err == nil || err.Error() != "unsupported local service operation" {
			t.Fatalf("removed operation %s returned %v", operation, err)
		}
	}
	profileAfter, err := os.ReadFile(m.profile())
	if err != nil || string(profileAfter) != string(profileBefore) {
		t.Fatal("removed proxy operations changed the Hub profile", err)
	}
}
