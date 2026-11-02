package store

import (
	"context"
	"path/filepath"
	"testing"

	"mewdex/internal/protocol"
)

func appendTaskPlanEvent(t *testing.T, s *Store, session protocol.Session, kind string, payload any) protocol.Event {
	t.Helper()
	event, err := s.Append(protocol.Event{ID: protocol.ID(), DeviceID: session.DeviceID, SessionID: session.ID, Kind: kind, Payload: protocol.JSON(payload), CreatedAt: protocol.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestTaskPlanSurvivesHistoryWindowReopenAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	session := protocol.Session{ID: "conversation", DeviceID: "windows"}
	appendTaskPlanEvent(t, s, session, "engine.turn/started", map[string]any{"turn": map[string]string{"id": "turn", "status": "inProgress"}})
	first := appendTaskPlanEvent(t, s, session, "engine.turn/plan/updated", map[string]any{"turnId": "turn", "explanation": nil, "plan": []protocol.TaskPlanStep{{Step: "检查布局", Status: "inProgress"}, {Step: "调整布局", Status: "pending"}}})
	latest := appendTaskPlanEvent(t, s, session, "engine.turn/plan/updated", map[string]any{"turnId": "turn", "explanation": "保留原输入", "plan": []protocol.TaskPlanStep{{Step: "检查布局", Status: "completed"}, {Step: "调整布局", Status: "inProgress"}}})
	for i := 0; i < 220; i++ {
		appendTaskPlanEvent(t, s, session, "engine.item/commandExecution/outputDelta", map[string]string{"delta": "long output"})
	}
	ended := appendTaskPlanEvent(t, s, session, "engine.turn/completed", map[string]any{"turn": map[string]string{"id": "turn", "status": "completed"}})
	page, err := s.EventPage(session.ID, 0, 200)
	if err != nil || !page.HasMore {
		t.Fatal("could not construct a bounded history window", page, err)
	}
	for _, event := range page.Events {
		if event.Kind == "engine.turn/plan/updated" {
			t.Fatal("fixture plan remained in the visible history window")
		}
	}
	other := protocol.Session{ID: "other", DeviceID: "mac"}
	appendTaskPlanEvent(t, s, other, "engine.turn/plan/updated", map[string]any{"turnId": "other-turn", "plan": []protocol.TaskPlanStep{{Step: "Other project", Status: "pending"}}})
	// Replayed old events retain their original sequence and cannot replace a newer plan.
	if _, err = s.Append(first); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.TaskPlan(ctx, session, 0)
	if err != nil || view == nil || view.Sequence != latest.Sequence || view.UpdatedAt != latest.CreatedAt || view.TurnStatus != "completed" || view.Explanation != "保留原输入" || len(view.Steps) != 2 || view.Steps[1].Status != "inProgress" {
		t.Fatalf("durable plan lost its actual steps or completion context: %+v %v", view, err)
	}
	view, err = s.TaskPlan(ctx, session, latest.Sequence)
	if err != nil || view == nil || view.Sequence != first.Sequence || view.Steps[0].Status != "inProgress" || view.TurnStatus != "inProgress" || view.Explanation != "" {
		t.Fatalf("exclusive historical cursor returned a later plan: %+v %v", view, err)
	}
	view, err = s.TaskPlan(ctx, session, ended.Sequence)
	if err != nil || view == nil || view.Sequence != latest.Sequence || view.TurnStatus != "inProgress" {
		t.Fatalf("future turn completion leaked into historical view: %+v %v", view, err)
	}
	for _, scope := range []protocol.Session{{ID: session.ID, DeviceID: "mac"}, {ID: "missing", DeviceID: session.DeviceID}} {
		if view, err = s.TaskPlan(ctx, scope, 0); err != nil || view != nil {
			t.Fatalf("plan crossed device or conversation: %+v %v", view, err)
		}
	}
}

func TestTaskPlanDoesNotCarryIntoNewTurnsAndEmptyPlanClearsSteps(t *testing.T) {
	s := memory(t)
	ctx := context.Background()
	session := protocol.Session{ID: "conversation", DeviceID: "windows"}
	appendTaskPlanEvent(t, s, session, "engine.turn/started", map[string]any{"turn": map[string]string{"id": "first", "status": "inProgress"}})
	appendTaskPlanEvent(t, s, session, "engine.turn/plan/updated", map[string]any{"turnId": "first", "plan": []protocol.TaskPlanStep{{Step: "原步骤", Status: "pending"}}})
	started := appendTaskPlanEvent(t, s, session, "engine.turn/started", map[string]any{"turn": map[string]string{"id": "second", "status": "inProgress"}})
	if view, err := s.TaskPlan(ctx, session, 0); err != nil || view != nil {
		t.Fatalf("new turn inherited old steps: %+v %v", view, err)
	}
	if view, err := s.TaskPlan(ctx, session, started.Sequence); err != nil || view == nil || view.TurnID != "first" {
		t.Fatalf("historical first turn was lost: %+v %v", view, err)
	}
	appendTaskPlanEvent(t, s, session, "engine.turn/plan/updated", map[string]any{"turnId": "second", "plan": []protocol.TaskPlanStep{{Step: "新步骤", Status: "inProgress"}}})
	cleared := appendTaskPlanEvent(t, s, session, "engine.turn/plan/updated", map[string]any{"turnId": "second", "plan": []protocol.TaskPlanStep{}})
	view, err := s.TaskPlan(ctx, session, 0)
	if err != nil || view == nil || view.Sequence != cleared.Sequence || view.Steps == nil || len(view.Steps) != 0 {
		t.Fatalf("empty plan restored old steps: %+v %v", view, err)
	}
}
