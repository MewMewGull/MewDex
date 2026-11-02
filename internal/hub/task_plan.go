package hub

import (
	"errors"
	"net/http"
	"strconv"

	"mewdex/internal/protocol"
)

func (h *Hub) taskPlan(w http.ResponseWriter, r *http.Request) {
	var session protocol.Session
	if h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session) != nil || !identity(r).Access.Allows(session.DeviceID, session.ProjectID) {
		problem(w, 404, errors.New("conversation not found"))
		return
	}
	before := int64(0)
	if value := r.URL.Query().Get("before"); value != "" {
		var err error
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before < 0 {
			problem(w, 400, errors.New("invalid plan cursor"))
			return
		}
	}
	view, err := h.store.TaskPlan(r.Context(), session, before)
	if err != nil {
		problem(w, 500, err)
		return
	}
	reply(w, 200, view)
}
