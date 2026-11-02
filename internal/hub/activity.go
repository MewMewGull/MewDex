package hub

import (
	"net/http"
	"sort"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func activityForSession(session protocol.Session, counts store.ActivityCounts) (protocol.SessionActivity, bool) {
	item := protocol.SessionActivity{
		Session:              session,
		PendingInteractions:  counts.PendingInteractions,
		BlockingInteractions: counts.BlockingInteractions,
		QueuedInputs:         counts.QueuedInputs,
		UnconfirmedInputs:    counts.UnconfirmedInputs,
		FailedInputs:         counts.FailedInputs,
	}
	if session.ExecutionRecovery != nil && session.ExecutionRecovery.State == "retired" {
		return item, false
	}
	item.NeedsAttention = counts.PendingInteractions > 0 || counts.UnconfirmedInputs > 0 || counts.FailedInputs > 0 ||
		session.State == "unknown" || session.StopStatus == "unconfirmed" || session.StopStatus == "processes-unchecked" ||
		session.Frozen && counts.QueuedInputs > 0
	active := item.NeedsAttention || session.State == "running" || session.State == "stopping" ||
		session.StopStatus == "background-running" || counts.QueuedInputs > 0
	return item, active
}

func (h *Hub) activity(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.visibleSessions(identity(r), "")
	if err != nil {
		problem(w, 500, err)
		return
	}
	counts, err := h.store.ActivityCounts()
	if err != nil {
		problem(w, 500, err)
		return
	}
	items := []protocol.SessionActivity{}
	for _, session := range sessions {
		if item, active := activityForSession(session, counts[session.ID]); active {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].NeedsAttention != items[j].NeedsAttention {
			return items[i].NeedsAttention
		}
		return items[i].Session.UpdatedAt > items[j].Session.UpdatedAt
	})
	reply(w, 200, items)
}

// Other conversations only need a refresh hint, not a form or queued message.
// The stored event and its cursor stay intact for scoped history and replay.
func activityStreamEvent(event protocol.Event, sessionID string, scoped bool) (protocol.Event, bool) {
	if streamEvent(event, sessionID, scoped) {
		return event, true
	}
	if protocol.ConversationReply(event) {
		event.Kind = "conversation.changed"
		event.Payload = protocol.JSON(protocol.ConversationUpdate{DeviceID: event.DeviceID, SessionID: event.SessionID, Sequence: event.Sequence})
		return event, true
	}
	if event.Kind != "interaction.updated" && event.Kind != "queue.item.updated" {
		return event, false
	}
	event.Kind = "activity.changed"
	event.Payload = protocol.JSON(map[string]string{"sessionId": event.SessionID})
	return event, true
}
