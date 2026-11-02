package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func TestTaskPlanReadUsesCurrentInputScopeAndHistoricalCursor(t *testing.T) {
	_, s, server, admin := testHub(t)
	for _, project := range []string{"work", "private", "empty"} {
		if err := s.Put("sessions", project, protocol.Session{ID: project, DeviceID: "node", ProjectID: project}); err != nil {
			t.Fatal(err)
		}
	}
	var planEvent protocol.Event
	for _, session := range []string{"work", "private"} {
		event, err := s.Append(protocol.Event{ID: protocol.ID(), DeviceID: "node", SessionID: session, Kind: "engine.turn/plan/updated", Payload: protocol.JSON(map[string]any{
			"threadId": "internal-thread", "turnId": "turn", "explanation": "visible explanation", "plan": []protocol.TaskPlanStep{{Step: "visible " + session, Status: "pending"}},
		})})
		if err != nil {
			t.Fatal(err)
		}
		if session == "work" {
			planEvent = event
		}
	}
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"work", "empty"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, actor, err := s.Pair(code, "reader", "")
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.URL, token, "GET", "/v1/task-plan?sessionId=work", nil)
	var view map[string]json.RawMessage
	if err = json.NewDecoder(response.Body).Decode(&view); err != nil || response.StatusCode != http.StatusOK || view["steps"] == nil || view["threadId"] != nil || view["payload"] != nil {
		t.Fatalf("authorized plan was unreadable or exposed raw engine fields: %+v %d %v", view, response.StatusCode, err)
	}
	for _, path := range []string{"/v1/task-plan?sessionId=empty", fmt.Sprintf("/v1/task-plan?sessionId=work&before=%d", planEvent.Sequence)} {
		response = request(t, server.URL, token, "GET", path, nil)
		var plan *protocol.TaskPlan
		if err = json.NewDecoder(response.Body).Decode(&plan); err != nil || response.StatusCode != http.StatusOK || plan != nil {
			t.Fatalf("missing or historical plan was not null: %+v %d %v", plan, response.StatusCode, err)
		}
	}
	for _, item := range []struct {
		token, path string
		status      int
	}{
		{"", "/v1/task-plan?sessionId=work", http.StatusUnauthorized},
		{token, "/v1/task-plan?sessionId=private", http.StatusNotFound},
		{token, "/v1/task-plan?sessionId=missing", http.StatusNotFound},
		{token, "/v1/task-plan?sessionId=work&before=-1", http.StatusBadRequest},
		{token, "/v1/task-plan?sessionId=work&before=invalid", http.StatusBadRequest},
	} {
		if response = request(t, server.URL, item.token, "GET", item.path, nil); response.StatusCode != item.status {
			t.Fatal("plan query access or cursor status", item.path, response.StatusCode, item.status)
		}
	}
	response = request(t, server.URL, admin, "GET", "/v1/bootstrap", nil)
	var snapshot protocol.NetworkSnapshot
	if err = json.NewDecoder(response.Body).Decode(&snapshot); err != nil || !slices.Contains(snapshot.Capabilities, "task-plans") {
		t.Fatalf("Hub did not advertise task plan support: %+v %v", snapshot.Capabilities, err)
	}
	if err = s.UpdateAccess(actor.ID, protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"private"}}}}); err != nil {
		t.Fatal(err)
	}
	if response = request(t, server.URL, token, "GET", "/v1/task-plan?sessionId=work", nil); response.StatusCode != http.StatusNotFound {
		t.Fatal("revoked project remained readable", response.StatusCode)
	}
	code, err = s.Invite("executor", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, err := s.Pair(code, "worker", "node")
	if err != nil {
		t.Fatal(err)
	}
	if response = request(t, server.URL, worker, "GET", "/v1/task-plan?sessionId=work", nil); response.StatusCode != http.StatusForbidden {
		t.Fatal("executor used the input plan endpoint", response.StatusCode)
	}
}
