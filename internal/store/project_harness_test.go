package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"mewdex/internal/projectharness"
	"mewdex/internal/protocol"
)

func projectHarnessFixture(t *testing.T) (*Store, protocol.Project) {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.RegisterDevice(protocol.Device{ID: "node", Projects: []protocol.Project{{ID: "root", Path: "/work"}, {ID: "other", Path: "/other"}}}); err != nil {
		t.Fatal(err)
	}
	project, err := s.SaveHubProject("hub", "hp_project", protocol.HubProjectRequest{Name: "Project", Directories: []protocol.ProjectDirectory{{DeviceID: "node", ProjectID: "root"}, {DeviceID: "node", ProjectID: "other"}}})
	if err != nil {
		t.Fatal(err)
	}
	return s, project
}

func TestProjectConfigurationRetriesRevertsAndConcurrentEditorsKeepImmutableHistory(t *testing.T) {
	s, project := projectHarnessFixture(t)
	initial, err := s.ProjectHarness(project.ID, 0)
	if err != nil || initial.Revision != 0 || initial.Configuration.Format != 1 {
		t.Fatal(initial, err)
	}
	input := protocol.ProjectHarnessUpdate{ExpectedProjectRevision: project.Revision, Configuration: projectharness.Empty()}
	input.Configuration.Instructions = "first exact instructions\n"
	first, err := s.SaveProjectHarness("hub", project.ID, input)
	if err != nil || first.Revision != 1 {
		t.Fatal(first, err)
	}
	retry, err := s.SaveProjectHarness("hub", project.ID, input)
	if err != nil || retry.Revision != first.Revision || retry.CreatedAt != first.CreatedAt {
		t.Fatal("retry created another activation", retry, err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for _, text := range []string{"editor one", "editor two"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			update := input
			update.ExpectedRevision = 1
			update.Configuration.Instructions = text
			_, err := s.SaveProjectHarness("hub", project.ID, update)
			results <- err
		}()
	}
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("concurrent editors lost an update", successes, conflicts)
	}
	input.ExpectedRevision = 2
	reverted, err := s.SaveProjectHarness("hub", project.ID, input)
	if err != nil || reverted.Revision != 3 || reverted.Version != first.Version {
		t.Fatal("revert lost content identity or activation history", reverted, err)
	}
	history, err := s.ProjectHarnessHistory(project.ID, 0)
	if err != nil || len(history.Items) != 3 || history.Items[0].Revision != 3 || history.Items[2].Revision != 1 {
		t.Fatal(history, err)
	}
	old, err := s.ProjectHarness(project.ID, 1)
	if err != nil || old.Configuration.Instructions != first.Configuration.Instructions {
		t.Fatal("rewrote old version", old, err)
	}
	corrupt := first.Configuration
	corrupt.Instructions = "modified immutable content"
	if err = s.Put("project_harness_versions", project.ID+":"+first.Version, corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectHarness(project.ID, 0); err == nil {
		t.Fatal("accepted a version with changed content")
	}
}

func TestProjectConfigurationBindingAndHistoryPagination(t *testing.T) {
	s, project := projectHarnessFixture(t)
	configuration := projectharness.Empty()
	configuration.Scripts = []protocol.ProjectScript{{ID: "check", Name: "Check", DeviceID: "node", ProjectID: "root", Command: []string{"check"}}}
	for revision := int64(0); revision < 23; revision++ {
		configuration.Instructions = fmt.Sprint(revision)
		_, err := s.SaveProjectHarness("hub", project.ID, protocol.ProjectHarnessUpdate{ExpectedRevision: revision, ExpectedProjectRevision: project.Revision, Configuration: configuration})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ProjectHarnessHistory(project.ID, 0)
	if err != nil || len(page.Items) != 20 || page.NextBefore != 4 {
		t.Fatal(page, err)
	}
	tail, err := s.ProjectHarnessHistory(project.ID, page.NextBefore)
	if err != nil || len(tail.Items) != 3 || tail.Items[0].Revision != 3 || tail.NextBefore != 0 {
		t.Fatal(tail, err)
	}
	if _, err = s.SaveHubProject("hub", project.ID, protocol.HubProjectRequest{Name: project.Name, ExpectedRevision: project.Revision, Directories: project.Directories[1:]}); err == nil {
		t.Fatal("removed a script's bound directory")
	}
	configuration.Instructions = "foreign directory"
	configuration.Scripts[0].ProjectID = "other"
	if _, err = s.SaveProjectHarness("hub", project.ID, protocol.ProjectHarnessUpdate{ExpectedRevision: 23, ExpectedProjectRevision: project.Revision + 1, Configuration: configuration}); !errors.Is(err, ErrConflict) {
		t.Fatal("accepted stale project layout", err)
	}
	history, _ := s.ProjectHarnessHistory(project.ID, 0)
	if history.Items[0].Revision != 23 {
		t.Fatal("rejected edit left a partial activation")
	}
}
