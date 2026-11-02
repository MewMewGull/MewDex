package localnode

import (
	"context"
	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/workspace"
	"testing"
)

func TestProjectFileLocationUsesConfiguredDeviceAndProject(t *testing.T) {
	m := testManager(t)
	project := t.TempDir()
	c := config.Config{DeviceID: "own-device", Name: "local", DataDir: "data", Hub: true, Projects: []protocol.Project{{ID: "allowed", Name: "Project", Path: project}}}
	if err := createJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{{Operation: "project.location", ProjectID: "allowed", ExpectedDeviceID: "another-device"}, {Operation: "project.location", ProjectID: "unknown", ExpectedDeviceID: c.DeviceID}, {Operation: "project.location", ProjectID: "allowed"}} {
		if _, err := m.Handle(context.Background(), request); err == nil {
			t.Fatal("unconfigured location was returned", request)
		}
	}
	result, err := m.Handle(context.Background(), Request{Operation: "project.location", ProjectID: "allowed", ExpectedDeviceID: c.DeviceID})
	canonical, pathErr := workspace.CanonicalDir(project)
	if err != nil || pathErr != nil || result.Project == nil || result.Project.Path != canonical {
		t.Fatal(result, err, pathErr)
	}
}
