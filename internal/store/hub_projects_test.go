package store

import (
	"errors"
	"path/filepath"
	"testing"

	"mewdex/internal/protocol"
)

func TestHubProjectGroupsPersistNativePathsAndRejectStaleOrForeignBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range []protocol.Device{{ID: "win", Projects: []protocol.Project{{ID: "game", Name: "Sott", Path: `D:\Projects\Sott`}, {ID: "engine", Name: "UE", Path: `D:\Engines\UE`}}}, {ID: "mac", Projects: []protocol.Project{{ID: "game", Name: "Sott", Path: "/Users/me/Sott"}}}} {
		if err = s.RegisterDevice(device); err != nil {
			t.Fatal(err)
		}
	}
	input := protocol.HubProjectRequest{Name: "Sott", Directories: []protocol.ProjectDirectory{{DeviceID: "win", ProjectID: "game"}, {DeviceID: "win", ProjectID: "engine"}, {DeviceID: "mac", ProjectID: "game"}}}
	project, err := s.SaveHubProject("hub", "hp_sott", input)
	if err != nil || project.Revision != 1 || len(project.Directories) != 3 || project.Directories[2].Path != "/Users/me/Sott" {
		t.Fatal(project, err)
	}
	retry, err := s.SaveHubProject("hub", "hp_sott", input)
	if err != nil || retry.Revision != 1 {
		t.Fatal("lost acknowledgment repeated the write", retry, err)
	}
	input.ExpectedRevision = project.Revision
	input.Name = "Sott desktop and servers"
	if _, err = s.SaveHubProject("hub", "hp_sott", input); err != nil {
		t.Fatal(err)
	}
	input.Name = "stale edit"
	if _, err = s.SaveHubProject("hub", "hp_sott", input); !errors.Is(err, ErrConflict) {
		t.Fatal("stale update replaced the central project", err)
	}
	input.ExpectedRevision = 2
	input.Directories[0].Path = "/path-on-wrong-machine"
	if _, err = s.SaveHubProject("hub", "hp_sott", input); err == nil {
		t.Fatal("foreign path replaced the node binding")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	projects, err := s.HubProjects()
	if err != nil || len(projects) != 1 || projects[0].Revision != 2 || projects[0].Directories[0].Path != `D:\Projects\Sott` {
		t.Fatal(projects, err)
	}
}
