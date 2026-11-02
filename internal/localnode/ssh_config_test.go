package localnode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/hubproxy"
)

func TestSSHReadIsBoundToCurrentHubAndDoesNotSaveOrEnableProxy(t *testing.T) {
	m := testManager(t)
	m.options.SSHHome = t.TempDir()
	directory := filepath.Join(m.options.SSHHome, ".ssh")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"config": "Host sample\n HostName server.example\n User deploy\n", "id_ed25519": "unread-private-key", "known_hosts": "unread-host-key"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	setup, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = hubproxy.Save(m.options.Root, hubproxy.Defaults()); err != nil {
		t.Fatal(err)
	}
	profileBefore, _ := os.ReadFile(m.profile())
	proxyPath := filepath.Join(m.options.Root, "hub-proxy.json")
	proxyBefore, _ := os.ReadFile(proxyPath)
	for _, id := range []string{"", "another-device"} {
		if _, err = m.Handle(ctx, Request{Operation: "proxy.ssh.config", ExpectedDeviceID: id}); err == nil {
			t.Fatal("an unbound read could access another installation's SSH metadata")
		}
	}
	result, err := m.Handle(ctx, Request{Operation: "proxy.ssh.config", ExpectedDeviceID: setup.Snapshot.DeviceID})
	if err != nil || result.SSHConfig == nil || len(result.SSHConfig.Profiles) != 1 || result.SSHConfig.Profiles[0].Host != "server.example" {
		t.Fatalf("current Hub cannot discover local SSH config: %#v / %v", result, err)
	}
	profileAfter, _ := os.ReadFile(m.profile())
	proxyAfter, _ := os.ReadFile(proxyPath)
	if string(profileBefore) != string(profileAfter) || string(proxyBefore) != string(proxyAfter) {
		t.Fatal("reading SSH metadata changed the active profile or proxy settings")
	}
	settings, _, _ := hubproxy.Read(m.options.Root)
	if settings.Enabled {
		t.Fatal("reading config enabled the reverse tunnel")
	}
}
