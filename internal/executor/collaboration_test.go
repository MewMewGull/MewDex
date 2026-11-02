package executor

import (
	"context"
	"errors"
	"testing"

	"mewdex/internal/protocol"
)

func TestCollaborationSavedModeAndHistoricalRestoration(t *testing.T) {
	x, _, fake := setup(t)
	ctx := context.Background()
	p := protocol.Policy{Mode: "focused", CollaborationMode: "plan"}
	created := x.Handle(ctx, protocol.Command{ID: "planning-create", DeviceID: "device", ProjectID: "project", SourceID: "phone", Kind: "create", Policy: &p})
	if created.State != "completed" {
		t.Fatal(created.Error)
	}
	s := *created.Session
	planningVersion, planningInstructions := s.PolicyVersion, s.PolicyInstructions
	start := func(id string, session protocol.Session) protocol.Session {
		t.Helper()
		result := x.Handle(ctx, protocol.Command{ID: id, DeviceID: session.DeviceID, SessionID: session.ID, SourceID: "phone", Kind: "start", Input: []protocol.Input{{Type: "text", Text: "continue"}}})
		if result.State != "completed" {
			t.Fatal(result.Error)
		}
		return *result.Session
	}
	s = start("planning-start", s)
	params := lastParameters(t, fake, "turn/start")
	mode := params["collaborationMode"].(map[string]any)
	settings := mode["settings"].(map[string]any)
	if mode["mode"] != "plan" || settings["developer_instructions"] != planningInstructions || settings["model"] != s.DefaultModel || params["approvalPolicy"] != "on-request" || params["approvalsReviewer"] != "user" {
		t.Fatalf("planning turn omitted mode, prompt or existing review scope: %+v", params)
	}
	finishTurn(x, s)
	p = s.Policy
	p.CollaborationMode = ""
	saved := x.Handle(ctx, protocol.Command{ID: "work-mode", DeviceID: s.DeviceID, SessionID: s.ID, SourceID: "phone", Kind: "policy.save", Policy: &p, ExpectedPolicyVersion: s.PolicyVersion})
	if saved.State != "completed" || saved.Session.PolicyVersion == planningVersion {
		t.Fatal("work mode was not saved separately", saved)
	}
	s = start("work-start", *saved.Session)
	if lastParameters(t, fake, "turn/start")["collaborationMode"].(map[string]any)["mode"] != "default" {
		t.Fatal("direct work did not restore the default engine mode")
	}
	finishTurn(x, s)
	restored := x.Handle(ctx, protocol.Command{ID: "restore-plan", DeviceID: s.DeviceID, SessionID: s.ID, SourceID: "phone", Kind: "policy.restore", RequestID: planningVersion, ExpectedPolicyVersion: s.PolicyVersion})
	if restored.State != "completed" || restored.Session.Policy.CollaborationMode != "plan" || restored.Session.PolicyInstructions != planningInstructions {
		t.Fatal("planning history was regenerated or lost", restored)
	}
	s = start("restored-planning-start", *restored.Session)
	mode = lastParameters(t, fake, "turn/start")["collaborationMode"].(map[string]any)
	if mode["mode"] != "plan" || mode["settings"].(map[string]any)["developer_instructions"] != planningInstructions {
		t.Fatal("restored mode was not used on the next turn", mode)
	}
	finishTurn(x, s)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, method := range fake.methods {
		if method == "thread/fork" {
			t.Fatal("a collaboration mode change forked the conversation")
		}
	}
}

func TestCollaborationQueueRetainsOriginalModeAfterLaterPolicyEdit(t *testing.T) {
	x, data, fake := setup(t)
	s := pausedQueue(t, x)
	p := s.Policy
	p.CollaborationMode = "plan"
	queued := addQueue(t, x, s, "planning-queue", &p)
	if queued.Policy.CollaborationMode != "plan" {
		t.Fatal("queued policy lost its planning mode", queued)
	}
	s.Frozen, s.State = false, "idle"
	setPolicy(&s, protocol.Policy{Mode: "full", Instructions: "changed after enqueue"})
	if err := x.save(&s); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.callErrors = map[string]error{"turn/start": errors.New("explicit fixture rejection")}
	fake.mu.Unlock()
	x.advance(s.ID)
	mode := lastParameters(t, fake, "turn/start")["collaborationMode"].(map[string]any)
	if mode["mode"] != "plan" || mode["settings"].(map[string]any)["developer_instructions"] != queued.PolicyInstructions {
		t.Fatal("later policy edits replaced the queued collaboration mode", mode)
	}
	var stored protocol.QueueItem
	if err := data.Get("queue_items", queued.ID, &stored); err != nil || stored.State != "failed" || stored.Policy.CollaborationMode != "plan" || stored.Input[0].Text != "planning-queue" {
		t.Fatal("dispatch failure lost the original planned input", stored, err)
	}
}
