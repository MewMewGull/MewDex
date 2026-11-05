package localnode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

func TestModelConnectionBelongsToHubAndPreservesProjectBindings(t *testing.T) {
	m := testManager(t)
	m.options.StartupBackend = &startupBackendFixture{}
	c := config.Config{DeviceID: "hub", Name: "Hub", Hub: true, Listen: "127.0.0.1:47830", DataDir: filepath.Join(m.options.Root, "data"), Projects: []protocol.Project{{ID: "p", Name: "Project", Path: t.TempDir()}}, Engine: config.Engine{Home: filepath.Join(m.options.Root, "engine-home"), Executable: m.options.Program}}
	if err := createJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	_, err := m.Handle(context.Background(), Request{Operation: "model-routing.apply", ExpectedDeviceID: c.DeviceID, ModelRouting: &ModelRoutingSettings{Route: "proxy", ProxyURL: "http://127.0.0.1:7890"}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := config.Read(m.profile())
	if err != nil || !reflect.DeepEqual(updated.Projects, c.Projects) || updated.Engine.ProxyURL != "http://127.0.0.1:7890" || updated.Engine.Home != c.Engine.Home || updated.Executor {
		t.Fatal(updated, err)
	}
	c.Hub, c.Executor, c.HubURL, c.NodeTokenFile = false, true, "https://hub.example.com", filepath.Join(m.options.Root, "node-token")
	if err := replaceJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(m.profile())
	_, err = m.Handle(context.Background(), Request{Operation: "model-routing.apply", ExpectedDeviceID: c.DeviceID, ModelRouting: &ModelRoutingSettings{Route: "proxy", ProxyURL: "http://127.0.0.1:7890"}})
	after, _ := os.ReadFile(m.profile())
	if err == nil || !bytes.Equal(before, after) || modelRouting(c).Route != "hub" {
		t.Fatal("execution node configured a model connection", err)
	}
}
