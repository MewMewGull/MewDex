package hub

import (
	"errors"
	"net/http"
	"strconv"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func (h *Hub) searchMessages(w http.ResponseWriter, r *http.Request) {
	var session protocol.Session
	if h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session) != nil || !identity(r).Access.Allows(session.DeviceID, session.ProjectID) {
		problem(w, 404, errors.New("conversation not found"))
		return
	}
	before, limit := int64(0), 30
	var err error
	if value := r.URL.Query().Get("before"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
	}
	if err != nil || before < 0 {
		problem(w, 400, errors.New("invalid search cursor"))
		return
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err != nil || limit < 1 || limit > 100 {
		problem(w, 400, errors.New("invalid search page size"))
		return
	}
	page, err := h.store.SearchMessages(r.Context(), session, r.URL.Query().Get("q"), before, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrInvalidSearch) {
			status = http.StatusBadRequest
		}
		problem(w, status, err)
		return
	}
	reply(w, 200, page)
}
