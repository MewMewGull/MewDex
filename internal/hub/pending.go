package hub

import (
	"errors"
	"net/http"
	"strconv"

	"mewdex/internal/protocol"
)

func (h *Hub) pendingCommands(w http.ResponseWriter, r *http.Request) {
	actor := identity(r)
	device := r.URL.Query().Get("deviceId")
	if device == "" || !actor.Access.Allows(device, "") {
		problem(w, 404, errors.New("device not found"))
		return
	}
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if r.URL.Query().Get("after") == "" {
		after, err = 0, nil
	}
	if err != nil || after < 0 {
		problem(w, 400, errors.New("invalid pending command cursor"))
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if r.URL.Query().Get("limit") == "" {
		limit, err = 20, nil
	}
	if err != nil || limit < 1 || limit > 200 {
		problem(w, 400, errors.New("pending command limit must be between 1 and 200"))
		return
	}
	page, err := h.store.PendingCommands(actor.ID, device, after, limit)
	if err != nil {
		problem(w, 500, err)
		return
	}
	visible := []protocol.CommandReference{}
	for _, reference := range page.Commands {
		command := protocol.Command{DeviceID: reference.DeviceID, SessionID: reference.SessionID, ProjectID: reference.ProjectID, Kind: reference.Kind}
		if h.allowsCommand(actor, command) {
			visible = append(visible, reference)
		}
	}
	page.Commands = visible
	reply(w, 200, page)
}
