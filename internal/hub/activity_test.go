package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func TestActivityScopedSnapshot(t *testing.T) {
	_, s, server, admin := testHub(t)
	for _, session := range []protocol.Session{
		{ID: "live", DeviceID: "node", ProjectID: "work", Title: "original", State: "running", UpdatedAt: "2026-10-29T00:00:02Z"},
		{ID: "paused", DeviceID: "node", ProjectID: "work", State: "paused", Frozen: true, UpdatedAt: "2026-10-29T00:00:01Z"},
		{ID: "private", DeviceID: "node", ProjectID: "private", State: "running"},
		{ID: "retired", DeviceID: "node", ProjectID: "work", State: "unknown", ExecutionRecovery: &protocol.ExecutionRecovery{State: "retired"}},
		{ID: "old-failure", DeviceID: "node", ProjectID: "work", State: "failed"},
	} {
		if err := s.Put("sessions", session.ID, session); err != nil {
			t.Fatal(err)
		}
	}
	title, archived := "working title", true
	if _, _, err := s.UpdateSession("live", protocol.SessionPatch{Title: &title, Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	for _, interaction := range []protocol.Interaction{
		{ID: "blocking", SessionID: "live", State: "pending", Blocking: true, Message: "request-body"},
		{ID: "nonblocking", SessionID: "live", State: "pending", Message: "request-body"},
		{ID: "answered", SessionID: "live", State: "answered"},
		{ID: "private", SessionID: "private", State: "pending"},
		{ID: "retired", SessionID: "retired", State: "pending"},
	} {
		if err := s.Put("interactions", interaction.ID, interaction); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []string{"queued", "dispatching", "unknown", "failed", "completed", "cancelled"} {
		if err := s.Put("queue_items", state, protocol.QueueItem{ID: state, SessionID: "live", State: state}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put("queue_items", "paused", protocol.QueueItem{ID: "paused", SessionID: "paused", State: "queued"}); err != nil {
		t.Fatal(err)
	}
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"work"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Pair(code, "input", "")
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.URL, token, "GET", "/v1/activity", nil)
	raw, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("activity query failed: %d %s %v", response.StatusCode, raw, err)
	}
	var items []protocol.SessionActivity
	if err := json.Unmarshal(raw, &items); err != nil || len(items) != 2 {
		t.Fatalf("scope or inactive filtering: %s %v", raw, err)
	}
	live := items[0]
	if live.Session.ID != "live" || live.Session.Title != title || !live.Session.Archived || !live.NeedsAttention ||
		live.PendingInteractions != 2 || live.BlockingInteractions != 1 || live.QueuedInputs != 2 || live.UnconfirmedInputs != 1 || live.FailedInputs != 1 {
		t.Fatalf("activity counts or presentation metadata: %+v", live)
	}
	if items[1].Session.ID != "paused" || !items[1].NeedsAttention || bytes.Contains(raw, []byte("request-body")) {
		t.Fatalf("paused queue or request disclosure: %s", raw)
	}
	adminResponse := request(t, server.URL, admin, "GET", "/v1/activity", nil)
	if err := json.NewDecoder(adminResponse.Body).Decode(&items); err != nil || len(items) != 3 {
		t.Fatalf("admin scope: %+v %v", items, err)
	}
	event := protocol.Event{ID: "event", Sequence: 42, DeviceID: "node", SessionID: "live", Kind: "interaction.updated", Payload: protocol.JSON(map[string]string{"message": "request-body"})}
	hint, include := activityStreamEvent(event, "other", true)
	if !include || hint.Kind != "activity.changed" || hint.Sequence != event.Sequence || hint.ID != event.ID || bytes.Contains(hint.Payload, []byte("request-body")) {
		t.Fatalf("unscoped activity hint: %+v", hint)
	}
	for _, selected := range []string{"live", ""} {
		original, include := activityStreamEvent(event, selected, selected != "")
		if !include || original.Kind != event.Kind || !bytes.Equal(original.Payload, event.Payload) {
			t.Fatal("selected conversation or notification subscription was changed")
		}
	}
}
