package hub

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func TestMessageSearchAndForwardHistoryRespectScope(t *testing.T) {
	_, s, server, _ := testHub(t)
	for _, project := range []string{"work", "private"} {
		if err := s.Put("sessions", project, protocol.Session{ID: project, DeviceID: "node", ProjectID: project}); err != nil {
			t.Fatal(err)
		}
	}
	for _, conversation := range []string{"work", "private", "work"} {
		_, err := s.Append(protocol.Event{ID: protocol.ID(), DeviceID: "node", SessionID: conversation, Kind: "message.user", Payload: protocol.JSON(map[string]any{"input": []protocol.Input{{Type: "text", Text: "needle " + conversation}}}), CreatedAt: protocol.Now()})
		if err != nil {
			t.Fatal(err)
		}
	}
	code, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"work"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, actor, err := s.Pair(code, "reader", "")
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server.URL, token, "GET", "/v1/message-search?sessionId=work&q=needle", nil)
	var page protocol.MessageSearchPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil || response.StatusCode != http.StatusOK || len(page.Matches) != 2 {
		t.Fatal("authorized conversation could not be searched", page, response.StatusCode, err)
	}
	response = request(t, server.URL, token, "GET", "/v1/session-events?sessionId=work&after=0&limit=1", nil)
	var history protocol.EventPage
	if err := json.NewDecoder(response.Body).Decode(&history); err != nil || len(history.Events) != 1 || history.HasMore || !history.HasNewer || history.After != history.Events[0].Sequence {
		t.Fatal("forward history did not expose stable navigation cursors", history, err)
	}
	for _, path := range []string{"/v1/message-search?sessionId=private&q=needle", "/v1/session-events?sessionId=private&after=0"} {
		if response = request(t, server.URL, token, "GET", path, nil); response.StatusCode != http.StatusNotFound {
			t.Fatal("outside project history was exposed", path, response.StatusCode)
		}
	}
	if response = request(t, server.URL, token, "GET", "/v1/message-search?sessionId=work&q=", nil); response.StatusCode != http.StatusBadRequest {
		t.Fatal("empty search was accepted", response.StatusCode)
	}
	if err = s.UpdateAccess(actor.ID, protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"private"}}}}); err != nil {
		t.Fatal(err)
	}
	if response = request(t, server.URL, token, "GET", "/v1/message-search?sessionId=work&q=needle", nil); response.StatusCode != http.StatusNotFound {
		t.Fatal("revoked project remained searchable", response.StatusCode)
	}
	code, err = s.Invite("executor", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, err := s.Pair(code, "worker", "node")
	if err != nil {
		t.Fatal(err)
	}
	if response = request(t, server.URL, worker, "GET", "/v1/message-search?sessionId=work&q=needle", nil); response.StatusCode != http.StatusForbidden {
		t.Fatal("executor used input history search", response.StatusCode)
	}
}
