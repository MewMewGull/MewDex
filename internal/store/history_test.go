package store

import (
	"strings"
	"testing"

	"mewdex/internal/protocol"
)

func TestHistoryPagesRemainStableWhileNewEventsArrive(t *testing.T) {
	s := memory(t)
	for i := 0; i < 7; i++ {
		_, err := s.Append(protocol.Event{ID: protocol.ID(), SessionID: "s", DeviceID: "node", Kind: "message.user", Payload: protocol.JSON(i)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Append(protocol.Event{ID: protocol.ID(), SessionID: "other", DeviceID: "node", Payload: protocol.JSON("other")})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.EventPage("s", 0, 3)
	if err != nil || len(page.Events) != 3 || !page.HasMore {
		t.Fatalf("%+v %v", page, err)
	}
	if _, err = s.Append(protocol.Event{ID: protocol.ID(), SessionID: "s", DeviceID: "node", Payload: protocol.JSON("new")}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for {
		for i, event := range page.Events {
			if event.SessionID != "s" || seen[event.ID] {
				t.Fatal("page crossed a conversation or repeated history")
			}
			if i > 0 && page.Events[i-1].Sequence >= event.Sequence {
				t.Fatal("history display order is not ascending")
			}
			seen[event.ID] = true
		}
		if !page.HasMore {
			break
		}
		page, err = s.EventPage("s", page.Before, 3)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 7 {
		t.Fatalf("concurrent insertion disturbed the older pages: %d", len(seen))
	}
}

func TestHistoryByteBudgetDoesNotStrandAnOversizedEvent(t *testing.T) {
	s := memory(t)
	for _, body := range []string{"older", strings.Repeat("x", 5<<20), "latest"} {
		if _, err := s.Append(protocol.Event{ID: protocol.ID(), SessionID: "s", Payload: protocol.JSON(body)}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.EventPage("s", 0, 200)
	if err != nil || len(page.Events) != 1 || !page.HasMore {
		t.Fatal("large history response was not bounded")
	}
	page, err = s.EventPage("s", page.Before, 200)
	if err != nil || len(page.Events) != 1 || !page.HasMore {
		t.Fatal("single oversized event is no longer retrievable")
	}
	page, err = s.EventPage("s", page.Before, 200)
	if err != nil || len(page.Events) != 1 || page.HasMore {
		t.Fatal("oversized event prevented access to earlier history")
	}
	page, err = s.EventPageAfter("s", 0, 200)
	if err != nil || len(page.Events) != 1 || !page.HasNewer || page.HasMore {
		t.Fatal("forward history did not begin at the oldest window", page, err)
	}
	page, err = s.EventPageAfter("s", page.After, 200)
	if err != nil || len(page.Events) != 1 || !page.HasNewer || !page.HasMore {
		t.Fatal("forward history stranded the oversized event", page, err)
	}
	page, err = s.EventPageAfter("s", page.After, 200)
	if err != nil || len(page.Events) != 1 || page.HasNewer || !page.HasMore {
		t.Fatal("forward history did not reach the latest window", page, err)
	}
}
