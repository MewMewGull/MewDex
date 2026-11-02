package executor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mewdex/internal/engine"
	"mewdex/internal/policy"
	"mewdex/internal/protocol"
)

func finishTurn(x *Executor, s protocol.Session) {
	x.consumeOne(engine.Notification{Method: "turn/completed", Params: protocol.JSON(map[string]any{"threadId": s.EngineID, "turn": map[string]string{"id": s.TurnID, "status": "completed"}})})
}
func lastParameters(t *testing.T, f *fixtureEngine, method string) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.methods) - 1; i >= 0; i-- {
		if f.methods[i] == method {
			var value map[string]any
			if err := json.Unmarshal(f.params[i], &value); err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	t.Fatalf("method was never called: %s", method)
	return nil
}

func TestSavedPolicyChangesTheNextTurnAndRejectsStaleInput(t *testing.T) {
	x, _, fake := setup(t)
	s := createAndStart(t, x)
	finishTurn(x, s)
	p := s.Policy
	p.Mode = "full"
	p.NoTests = true
	r := x.Handle(context.Background(), protocol.Command{ID: "save", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "policy.save", Policy: &p, ExpectedPolicyVersion: s.PolicyVersion})
	if r.State != "completed" {
		t.Fatal(r.Error)
	}
	stale := x.Handle(context.Background(), protocol.Command{ID: "stale-save", SourceID: "desktop", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "policy.save", Policy: &s.Policy, ExpectedPolicyVersion: s.PolicyVersion})
	if stale.State != "failed" {
		t.Fatal("stale input overwrote another input's policy")
	}
	r = x.Handle(context.Background(), protocol.Command{ID: "next", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", Input: []protocol.Input{{Type: "text", Text: "continue"}}})
	if r.State != "completed" {
		t.Fatal(r.Error)
	}
	params := lastParameters(t, fake, "turn/start")
	mode := params["collaborationMode"].(map[string]any)
	settings := mode["settings"].(map[string]any)
	if !strings.Contains(settings["developer_instructions"].(string), "不要测试") || settings["model"] != "fixture-model" || params["approvalsReviewer"] != "user" {
		t.Fatalf("next turn did not receive its policy: %v", params)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, method := range fake.methods {
		if method == "thread/resume" {
			t.Fatal("per-turn policy used a no-op live resume")
		}
	}
}

func TestRestoreUsesExactHistoricalInstructions(t *testing.T) {
	x, store, _ := setup(t)
	s := createAndStart(t, x)
	finishTurn(x, s)
	text := "EXACT historical instructions, from an earlier application template"
	version := policy.VersionFor(s.Policy, text)
	snapshot := protocol.PolicySnapshot{ID: s.ID + ":" + version, SessionID: s.ID, Version: version, Policy: s.Policy, DeveloperInstructions: text, CreatedAt: protocol.Now()}
	if err := store.Put("policies", snapshot.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	r := x.Handle(context.Background(), protocol.Command{ID: "restore", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "policy.restore", RequestID: version, ExpectedPolicyVersion: s.PolicyVersion})
	if r.State != "completed" || r.Session.PolicyInstructions != text {
		t.Fatalf("restore: %+v", r)
	}
	r = x.Handle(context.Background(), protocol.Command{ID: "after-restore", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", Policy: &s.Policy, Input: []protocol.Input{{Type: "text", Text: "continue"}}})
	if r.State != "completed" || r.Session.PolicyVersion != version || r.Session.PolicyInstructions != text {
		t.Fatal("restored instructions were regenerated from the current template")
	}
}

func TestBaseRestoreUsesTheInitialSnapshot(t *testing.T) {
	x, _, fake := setup(t)
	s := createAndStart(t, x)
	finishTurn(x, s)
	base := "replacement base"
	p := s.Policy
	p.BaseInstructions = &base
	r := x.Handle(context.Background(), protocol.Command{ID: "custom", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", Policy: &p, Input: []protocol.Input{{Type: "text", Text: "continue"}}})
	if r.State != "completed" {
		t.Fatal(r.Error)
	}
	if lastParameters(t, fake, "thread/fork")["baseInstructions"] != base {
		t.Fatal("base replacement was not applied")
	}
	finishTurn(x, *r.Session)
	r = x.Handle(context.Background(), protocol.Command{ID: "inherit-original", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", Policy: &s.Policy, Input: []protocol.Input{{Type: "text", Text: "continue"}}})
	if r.State != "completed" {
		t.Fatal(r.Error)
	}
	if lastParameters(t, fake, "thread/fork")["baseInstructions"] != "fixture initial base instructions" {
		t.Fatal("reset inherited the replacement instead of restoring the original base")
	}
}
