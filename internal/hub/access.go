package hub

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/coder/websocket"
	"mewdex/internal/protocol"
)

func (h *Hub) allowsCommand(id protocol.Identity, c protocol.Command) bool {
	if strings.HasPrefix(c.Kind, "terminal.") || c.Terminal != nil || strings.HasPrefix(c.Kind, "computer.") || c.Computer != nil {
		return false
	}
	if strings.HasPrefix(c.Kind, "workspace.") && c.DeviceID == h.coordinatorID {
		return false
	}
	if h.coordinator != nil && !strings.HasPrefix(c.Kind, "workspace.") && c.DeviceID != h.coordinatorID {
		return false
	}
	if (strings.HasPrefix(c.Kind, "account.") || strings.HasPrefix(c.Kind, "workspace.")) && id.Role != "admin" {
		return false
	}
	if c.SessionID != "" {
		var s protocol.Session
		if h.store.Get("sessions", c.SessionID, &s) != nil || s.DeviceID != c.DeviceID {
			return false
		}
		return id.Access.Allows(s.DeviceID, s.ProjectID)
	}
	return id.Access.Allows(c.DeviceID, c.ProjectID)
}

// Check the original incoming reply before its body is removed from the
// durable command ledger. Receipt visibility only needs the target scope.
func (h *Hub) allowsInteractionAcceptance(id protocol.Identity, c protocol.Command) bool {
	if c.Kind != "respond" {
		return true
	}
	var reply protocol.InteractionReply
	if json.Unmarshal(c.Response, &reply) != nil || reply.Action != "accept" {
		return true
	}
	var request protocol.Interaction
	if h.store.Get("interactions", c.RequestID, &request) != nil || request.SessionID != c.SessionID {
		return false
	}
	view := interactionForIdentity(id, request)
	return view.CanAccept
}

func interactionForIdentity(id protocol.Identity, view protocol.Interaction) protocol.Interaction {
	if !view.CanAccept {
		return view
	}
	reason := ""
	if (view.Kind == "command" || view.Kind == "files" || view.Kind == "permissions") && view.ExecutionScopeID == "" {
		reason = "此执行端尚未确认会话权限范围，请更新执行端。"
	}
	if id.Role != "admin" && (view.Kind == "permissions" || view.Permissions != nil) {
		reason = "额外权限需要管理员输入端确认。"
	}
	if reason != "" {
		view.CanAccept = false
		if view.Details != "" {
			view.Details += "\n"
		}
		view.Details += reason
	}
	return view
}

func interactionEventForIdentity(id protocol.Identity, event protocol.Event) protocol.Event {
	if event.Kind == "interaction.updated" {
		var view protocol.Interaction
		if json.Unmarshal(event.Payload, &view) == nil {
			event.Payload = protocol.JSON(interactionForIdentity(id, view))
		}
	}
	return event
}

func (h *Hub) visibleEvent(id protocol.Identity, e protocol.Event) bool {
	if strings.HasPrefix(e.Kind, "terminal.") || strings.HasPrefix(e.Kind, "computer.") {
		return false
	}
	if e.Kind == "project.catalog.updated" || e.Kind == "project.harness.updated" {
		var project struct {
			ProjectID string `json:"projectId"`
		}
		return json.Unmarshal(e.Payload, &project) == nil && id.Access.Allows(h.coordinatorID, project.ProjectID)
	}
	if e.Kind == "workspace.catalog.updated" && id.Role != "admin" {
		return false
	}
	if strings.HasPrefix(e.Kind, "engine.account/") && id.Role != "admin" {
		return false
	}
	if e.SessionID != "" {
		var s protocol.Session
		if h.store.Get("sessions", e.SessionID, &s) != nil {
			return false
		}
		return id.Access.Allows(s.DeviceID, s.ProjectID)
	}
	if e.Kind == "command.result" {
		var result protocol.CommandResult
		if json.Unmarshal(e.Payload, &result) != nil {
			return false
		}
		cmd, err := h.store.Command(result.CommandID)
		return err == nil && h.allowsCommand(id, cmd)
	}
	return id.Access.Allows(e.DeviceID, "")
}

func (h *Hub) disconnectIdentity(id string) {
	h.mu.Lock()
	connections := []*websocket.Conn{}
	peers := []*peer{}
	for _, p := range h.peers {
		if p.identityID == id {
			peers = append(peers, p)
		}
	}
	for conn := range h.subscribers[id] {
		connections = append(connections, conn)
	}
	for _, bridge := range h.inputBridges {
		if bridge.owner == id {
			connections = append(connections, bridge.conn)
		}
	}
	close(h.wake)
	h.wake = make(chan struct{})
	h.mu.Unlock()
	for _, p := range peers {
		p.cancel()
		_ = p.conn.CloseNow()
	}
	for _, conn := range connections {
		_ = conn.CloseNow()
	}
}

func (h *Hub) registerSubscriber(id string, conn *websocket.Conn) func() {
	h.mu.Lock()
	if h.subscribers[id] == nil {
		h.subscribers[id] = map[*websocket.Conn]bool{}
	}
	h.subscribers[id][conn] = true
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.subscribers[id], conn)
		if len(h.subscribers[id]) == 0 {
			delete(h.subscribers, id)
		}
		h.mu.Unlock()
	}
}

func validateAccess(access protocol.Access) error {
	seen := map[string]bool{}
	for _, target := range access.Targets {
		if target.DeviceID == "" || seen[target.DeviceID] {
			return errors.New("target device IDs must be non-empty and unique")
		}
		seen[target.DeviceID] = true
		projects := map[string]bool{}
		for _, project := range target.ProjectIDs {
			if project == "" || projects[project] {
				return errors.New("project IDs must be non-empty and unique")
			}
			projects[project] = true
		}
	}
	return nil
}
