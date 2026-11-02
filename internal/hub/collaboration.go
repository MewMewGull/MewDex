package hub

import (
	"slices"

	"mewdex/internal/protocol"
)

// Older executors ignore unknown policy fields. A planning request must not be
// delivered to one as an ordinary execution request.
func (h *Hub) unsupportedCollaboration(c protocol.Command) bool {
	switch c.Kind {
	case "create", "start", "steer", "enqueue", "policy.save", "policy.inspect", "policy.restore":
	default:
		return false
	}
	mode := ""
	if c.Policy != nil {
		mode = c.Policy.CollaborationMode
	} else if c.SessionID != "" {
		var session protocol.Session
		if h.store.Get("sessions", c.SessionID, &session) == nil {
			mode = session.Policy.CollaborationMode
		}
	}
	if c.Kind == "policy.restore" {
		var snapshot protocol.PolicySnapshot
		if h.store.Get("policies", c.SessionID+":"+c.RequestID, &snapshot) == nil {
			mode = snapshot.Policy.CollaborationMode
		}
	}
	if mode != "plan" {
		return false
	}
	var device protocol.Device
	return h.store.Get("devices", c.DeviceID, &device) != nil || !slices.Contains(device.Capabilities, "collaboration-modes")
}
