package executor

import (
	"context"
	"strings"
	"testing"

	"mewdex/internal/protocol"
)

func TestCachedForeignAttachmentCannotStartQueueOrSteer(t *testing.T) {
	x, state, fake := setup(t)
	foreign, err := x.attachments.SaveTarget("other-project.txt", strings.NewReader("other project input"), "client", "device", "another-project")
	if err != nil {
		t.Fatal(err)
	}
	valid, err := x.attachments.SaveTarget("current-project.txt", strings.NewReader("current project input"), "client", "device", "project")
	if err != nil {
		t.Fatal(err)
	}
	created := x.Handle(context.Background(), protocol.Command{ID: "attachment-create", DeviceID: "device", ProjectID: "project", SourceID: "client", Kind: "create"})
	if created.State != "completed" {
		t.Fatal(created.Error)
	}
	session := created.Session
	for _, kind := range []string{"start", "enqueue"} {
		result := x.Handle(context.Background(), protocol.Command{ID: "foreign-" + kind, DeviceID: "device", SessionID: session.ID, SourceID: "client", Kind: kind, Input: []protocol.Input{{Type: "file", AttachmentID: foreign.ID}}})
		if result.State != "failed" || !strings.Contains(result.Error, "execution device and project") {
			t.Fatalf("%s: %+v", kind, result)
		}
	}
	items, err := state.QueueItems(session.ID)
	if err != nil || len(items) != 0 {
		t.Fatal("foreign attachment entered the queue", err)
	}
	started := x.Handle(context.Background(), protocol.Command{ID: "valid-start", DeviceID: "device", SessionID: session.ID, SourceID: "client", Kind: "start", Input: []protocol.Input{{Type: "file", AttachmentID: valid.ID}}})
	if started.State != "completed" {
		t.Fatal(started.Error)
	}
	steered := x.Handle(context.Background(), protocol.Command{ID: "foreign-steer", DeviceID: "device", SessionID: session.ID, SourceID: "client", Kind: "steer", Input: []protocol.Input{{Type: "file", AttachmentID: foreign.ID}}})
	if steered.State != "failed" || !strings.Contains(steered.Error, "execution device and project") {
		t.Fatalf("steer: %+v", steered)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	starts := 0
	for _, method := range fake.methods {
		if method == "turn/start" {
			starts++
		}
		if method == "turn/steer" {
			t.Fatal("foreign attachment reached the engine")
		}
	}
	if starts != 1 {
		t.Fatalf("engine received %d starts, want the single valid input", starts)
	}
}
