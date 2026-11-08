package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestRemoveHubProjectPreservesConversationsAndBindingsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(file, []byte("keep project files"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	device := protocol.Device{ID: "node", Projects: []protocol.Project{{ID: "directory", Name: "Files", Path: root}}}
	if err = s.RegisterDevice(device); err != nil {
		t.Fatal(err)
	}
	input := protocol.HubProjectRequest{Name: "Remove me", Directories: []protocol.ProjectDirectory{{DeviceID: device.ID, ProjectID: "directory"}}}
	project, err := s.SaveHubProject("hub", "hp_remove", input)
	if err != nil {
		t.Fatal(err)
	}
	session := protocol.Session{ID: "chat", DeviceID: "hub", ProjectID: project.ID, State: "running", Pinned: true}
	if err = s.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RemoveHubProject("hub", project.ID, 0); !errors.Is(err, ErrConflict) {
		t.Fatal("stale removal was accepted", err)
	}
	before, err := s.LatestSequence()
	if err != nil {
		t.Fatal(err)
	}
	removed, err := s.RemoveHubProject("hub", project.ID, project.Revision)
	if err != nil || !removed.Removed || removed.Revision != project.Revision+1 || !reflect.DeepEqual(removed.Directories, project.Directories) {
		t.Fatal(removed, err)
	}
	if retry, err := s.RemoveHubProject("hub", project.ID, project.Revision); err != nil || !reflect.DeepEqual(retry, removed) {
		t.Fatal("retry changed the removal", retry, err)
	}
	if after, err := s.LatestSequence(); err != nil || after != before+1 {
		t.Fatal("removal event was missing or duplicated", before, after, err)
	}
	if _, err = s.SaveHubProject("hub", project.ID, input); !errors.Is(err, ErrNotFound) {
		t.Fatal("a stale editor resurrected the project", err)
	}
	if err = s.Put("settings", "hub_projects_imported", true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ImportHubProjects("hub"); err != nil {
		t.Fatal(err)
	}
	var persisted protocol.Project
	if err = s.Get("hub_projects", project.ID, &persisted); err != nil || !reflect.DeepEqual(persisted, removed) {
		t.Fatal("removed project did not persist", persisted, err)
	}
	var chat protocol.Session
	if err = s.Get("sessions", session.ID, &chat); err != nil || !reflect.DeepEqual(chat, session) {
		t.Fatal("removal changed an existing conversation", chat, err)
	}
	var node protocol.Device
	if err = s.Get("devices", device.ID, &node); err != nil || !reflect.DeepEqual(node.Projects, device.Projects) {
		t.Fatal("removal changed the executor directory", node, err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep project files" {
		t.Fatal("removal affected project files", err)
	}
}
