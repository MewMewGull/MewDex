package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func (h *Hub) updateSession(w http.ResponseWriter, r *http.Request) {
	id := identity(r)
	if id.Role != "client" && id.Role != "admin" {
		problem(w, 403, errors.New("input identity required"))
		return
	}
	sessionID := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
	var session protocol.Session
	if h.store.Get("sessions", sessionID, &session) != nil || !id.Access.Allows(session.DeviceID, session.ProjectID) {
		problem(w, 404, errors.New("session not found"))
		return
	}
	var patch protocol.SessionPatch
	if !decode(w, r, &patch) {
		return
	}
	view, changed, err := h.store.UpdateSession(sessionID, patch)
	if err != nil {
		status := 500
		if errors.Is(err, store.ErrInvalidSessionTitle) {
			status = 400
		} else if errors.Is(err, store.ErrNotFound) {
			status = 404
		}
		problem(w, status, err)
		return
	}
	if changed {
		h.notify()
	}
	reply(w, 200, view)
}

func (h *Hub) resultView(result protocol.CommandResult) (protocol.CommandResult, error) {
	if result.Session != nil {
		session, err := h.store.SessionView(*result.Session)
		if err != nil {
			return result, err
		}
		result.Session = &session
	}
	return result, nil
}

func (h *Hub) replyResult(w http.ResponseWriter, status int, result protocol.CommandResult) {
	view, err := h.resultView(result)
	if err != nil {
		problem(w, 500, err)
		return
	}
	reply(w, status, view)
}

// Projection only changes the input view; executor events and receipts stay intact.
func (h *Hub) inputEvent(id protocol.Identity, event protocol.Event) (protocol.Event, error) {
	event = interactionEventForIdentity(id, event)
	switch event.Kind {
	case "workspace.catalog.updated":
		var device protocol.Device
		if err := h.store.Get("devices", event.DeviceID, &device); err != nil {
			return event, err
		}
		event.Payload = protocol.JSON(device.Projects)
	case "session.updated":
		var session protocol.Session
		if err := json.Unmarshal(event.Payload, &session); err != nil {
			return event, err
		}
		view, err := h.store.SessionView(session)
		if err != nil {
			return event, err
		}
		event.Payload = protocol.JSON(view)
	case "command.result":
		var result protocol.CommandResult
		if err := json.Unmarshal(event.Payload, &result); err != nil {
			return event, err
		}
		view, err := h.resultView(result)
		if err != nil {
			return event, err
		}
		event.Payload = protocol.JSON(view)
	}
	return event, nil
}
