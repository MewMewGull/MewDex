package executor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestSameProjectRejectsConcurrentWritingTurns(t *testing.T) {
	x, _, _ := setup(t)
	createAndStart(t, x)
	created := x.Handle(context.Background(), protocol.Command{ID: "second-session", DeviceID: "device", ProjectID: "project", Kind: "create", SourceID: "client"})
	if created.State != "completed" {
		t.Fatal(created.Error)
	}
	started := x.Handle(context.Background(), protocol.Command{ID: "second-turn", DeviceID: "device", SessionID: created.Session.ID, Kind: "start", SourceID: "client", Input: []protocol.Input{{Type: "text", Text: "concurrent change"}}})
	if started.State != "failed" {
		t.Fatalf("concurrent writing turn started: %+v", started)
	}
}

func TestDifferentProjectIDsCannotWriteOverlappingRoots(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "same directory", true: "nested directory"}[nested], func(t *testing.T) {
			root := t.TempDir()
			secondRoot := root
			if nested {
				secondRoot = filepath.Join(root, "child")
				if err := os.Mkdir(secondRoot, 0700); err != nil {
					t.Fatal(err)
				}
			}
			s, err := store.Open(filepath.Join(t.TempDir(), "executor.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			fake := &fixtureEngine{notifications: make(chan engine.Notification, 10)}
			x, err := New(config.Config{DeviceID: "device", DataDir: t.TempDir(), Projects: []protocol.Project{{ID: "project", Path: root}, {ID: "alias", Path: secondRoot}}}, s, fake, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer x.Close()
			createAndStart(t, x)
			created := x.Handle(context.Background(), protocol.Command{ID: "create-alias", DeviceID: "device", ProjectID: "alias", Kind: "create", SourceID: "client"})
			if created.Session == nil {
				t.Fatal(created.Error)
			}
			started := x.Handle(context.Background(), protocol.Command{ID: "start-alias", DeviceID: "device", SessionID: created.Session.ID, Kind: "start", SourceID: "client", Input: []protocol.Input{{Type: "text", Text: "must not run in parallel"}}})
			if started.State != "failed" {
				t.Fatalf("overlapping task was submitted: %+v", started)
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			starts := 0
			for _, method := range fake.methods {
				if method == "turn/start" {
					starts++
				}
			}
			if starts != 1 {
				t.Fatal("overlapping root reached the engine")
			}
		})
	}
}

func TestWorkspaceBindingCannotBeRelabeledAfterRestart(t *testing.T) {
	root, other, data := t.TempDir(), t.TempDir(), t.TempDir()
	s, err := store.Open(filepath.Join(data, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := config.Config{DeviceID: "device", DataDir: data, Projects: []protocol.Project{{ID: "project", Path: root}}}
	x, err := New(c, s, &fixtureEngine{notifications: make(chan engine.Notification, 1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = x.Close(); err != nil {
		t.Fatal(err)
	}
	c.Projects = []protocol.Project{{ID: "project", Path: other}}
	if _, err = New(c, s, &fixtureEngine{notifications: make(chan engine.Notification, 1)}, nil); err == nil {
		t.Fatal("the same project ID moved existing conversations to a different directory")
	}
}

func TestRestartRetainsEveryUncertainWorkspaceOwner(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	s, err := store.Open(filepath.Join(data, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"first", "second"} {
		if err = s.Put("sessions", id, protocol.Session{ID: id, DeviceID: "device", ProjectID: "project", State: "unknown", EngineStarted: true}); err != nil {
			t.Fatal(err)
		}
	}
	x, err := New(config.Config{DeviceID: "device", DataDir: data, Projects: []protocol.Project{{ID: "project", Path: root}}}, s, &fixtureEngine{notifications: make(chan engine.Notification, 1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	x.releaseProject(&protocol.Session{ID: "second", ProjectID: "project"})
	if err = x.claimProject(&protocol.Session{ID: "new", ProjectID: "project"}); err == nil {
		t.Fatal("releasing one unknown task unlocked another task's workspace")
	}
}
