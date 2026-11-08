package hub

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

// Optional filters reduce background input traffic. They are never an access
// grant: every event still passes the current identity and session checks.
func eventKinds(query url.Values) (map[string]bool, error) {
	values, present := query["kind"]
	if !present {
		return nil, nil
	}
	if len(values) == 0 || len(values) > 16 {
		return nil, errors.New("invalid event kind filter")
	}
	kinds := make(map[string]bool, len(values))
	for _, value := range values {
		if len(value) == 0 || len(value) > 128 {
			return nil, errors.New("invalid event kind filter")
		}
		for _, c := range []byte(value) {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '/' || c == '-') {
				return nil, errors.New("invalid event kind filter")
			}
		}
		kinds[value] = true
	}
	return kinds, nil
}

func (h *Hub) visibleDevices(id protocol.Identity) ([]protocol.Device, error) {
	devices, err := store.List[protocol.Device](h.store, "devices")
	if err != nil {
		return nil, err
	}
	hubProjects := []protocol.Project{}
	if h.coordinatorID != "" {
		hubProjects, err = h.store.HubProjects()
		if err != nil {
			return nil, err
		}
	}
	// Project readers need the presence of their execution nodes. The projection
	// includes only that project's bindings and grants no direct node access.
	linked := map[string]map[string]bool{}
	for _, project := range hubProjects {
		if !id.Access.Allows(h.coordinatorID, project.ID) {
			continue
		}
		for _, directory := range project.Directories {
			if linked[directory.DeviceID] == nil {
				linked[directory.DeviceID] = map[string]bool{}
			}
			linked[directory.DeviceID][directory.ProjectID] = true
		}
	}
	visible := []protocol.Device{}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, device := range devices {
		if !id.Access.Allows(device.ID, "") && len(linked[device.ID]) == 0 {
			continue
		}
		device.Online = device.ID == h.coordinatorID && h.coordinator != nil || device.ID == h.config.DeviceID && h.local != nil || h.peers[device.ID] != nil
		if device.ID == h.coordinatorID {
			device.Projects = hubProjects
		}
		projects := []protocol.Project{}
		for _, project := range device.Projects {
			if id.Access.Allows(device.ID, project.ID) || linked[device.ID][project.ID] {
				projects = append(projects, project)
			}
		}
		device.Projects = projects
		visible = append(visible, device)
	}
	return visible, nil
}

func (h *Hub) visibleSessions(id protocol.Identity, device string) ([]protocol.Session, error) {
	sessions, err := store.List[protocol.Session](h.store, "sessions")
	if err != nil {
		return nil, err
	}
	visible := []protocol.Session{}
	for _, session := range sessions {
		if id.Access.Allows(session.DeviceID, session.ProjectID) && (device == "" || session.DeviceID == device) {
			visible = append(visible, session)
		}
	}
	return h.store.SessionViews(visible)
}

func (h *Hub) bootstrap(w http.ResponseWriter, r *http.Request) {
	// Read the high-water mark first: events that race the following projections
	// are replayed, so a newly connected input never loses a concurrent update.
	cursor, err := h.store.LatestSequence()
	if err != nil {
		problem(w, 500, err)
		return
	}
	id := identity(r)
	devices, err := h.visibleDevices(id)
	if err != nil {
		problem(w, 500, err)
		return
	}
	sessions, err := h.visibleSessions(id, "")
	if err != nil {
		problem(w, 500, err)
		return
	}
	updates, err := h.store.ConversationUpdates(r.Context())
	if err != nil {
		problem(w, 500, err)
		return
	}
	visible := make(map[string]string, len(sessions))
	for _, session := range sessions {
		visible[session.ID] = session.DeviceID
	}
	conversationUpdates := []protocol.ConversationUpdate{}
	for _, update := range updates {
		if device, ok := visible[update.SessionID]; ok && device == update.DeviceID {
			conversationUpdates = append(conversationUpdates, update)
		}
	}
	capabilities := []string{"task-plans", "collaboration-modes", "mewdex-accounts", "central-projects"}
	if h.coordinator != nil {
		capabilities = append(capabilities, "hub-projects", "hub-model-account")
	}
	reply(w, 200, protocol.NetworkSnapshot{Cursor: cursor, Identity: id, Devices: devices, Sessions: sessions, ConversationUpdates: conversationUpdates, Capabilities: capabilities})
}

func (h *Hub) historyPage(w http.ResponseWriter, r *http.Request) {
	var session protocol.Session
	if h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session) != nil || !identity(r).Access.Allows(session.DeviceID, session.ProjectID) {
		problem(w, 404, errors.New("session not found"))
		return
	}
	before := int64(0)
	var err error
	if value := r.URL.Query().Get("before"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
	}
	if err != nil || before < 0 {
		problem(w, 400, errors.New("invalid history cursor"))
		return
	}
	after := int64(0)
	_, forward := r.URL.Query()["after"]
	if forward {
		after, err = strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if err != nil || after < 0 || before != 0 {
			problem(w, 400, errors.New("choose one valid history direction"))
			return
		}
	}
	limit := 200
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err != nil || limit < 1 || limit > 1000 {
		problem(w, 400, errors.New("invalid history page size"))
		return
	}
	var page protocol.EventPage
	if forward {
		page, err = h.store.EventPageAfter(session.ID, after, limit)
	} else {
		page, err = h.store.EventPage(session.ID, before, limit)
	}
	if err != nil {
		problem(w, 500, err)
		return
	}
	// A page only contains this conversation. Account-wide events never enter it.
	visible := make([]protocol.Event, 0, len(page.Events))
	for _, event := range page.Events {
		if h.visibleEvent(identity(r), event) {
			view, err := h.inputEvent(identity(r), event)
			if err != nil {
				problem(w, 500, err)
				return
			}
			visible = append(visible, view)
		}
	}
	page.Events = visible
	reply(w, 200, page)
}

func streamEvent(event protocol.Event, session string, scoped bool) bool {
	if !scoped || event.SessionID == "" || event.Kind == "session.updated" || event.Kind == "session.metadata.updated" || event.Kind == "command.result" {
		return true
	}
	return event.SessionID == session
}
