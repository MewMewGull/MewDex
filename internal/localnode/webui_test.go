package localnode

import (
	"bytes"
	"context"
	"mewdex/internal/webui"
	"os"
	"testing"
)

func TestLegacyWebPreferenceRequiresStandaloneDeploymentAndKeepsItsProfile(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	setup, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true, Name: "web-entry", Listen: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range []string{"", "another-device"} {
		if _, err := m.Handle(ctx, Request{Operation: "web.apply", ExpectedDeviceID: device, WebWorkbench: &WebWorkbenchSettings{Enabled: true}}); err == nil {
			t.Fatal("web enabled without the local device identity")
		}
		if webui.Enabled(m.options.Root) {
			t.Fatal("rejected write changed web settings")
		}
	}
	for _, enabled := range []bool{true, false} {
		result, err := m.Handle(ctx, Request{Operation: "web.apply", ExpectedDeviceID: setup.Snapshot.DeviceID, WebWorkbench: &WebWorkbenchSettings{Enabled: enabled}})
		if err == nil || result.WebWorkbench != nil || webui.Enabled(m.options.Root) {
			t.Fatal("a separate Hub still enabled an embedded web input", err)
		}
	}
	after, err := os.ReadFile(m.profile())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("web preference modified the device profile", err)
	}
}
