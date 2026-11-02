package hub

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

type collaborationRunner struct{ calls chan protocol.Command }

func (r collaborationRunner) Handle(_ context.Context, c protocol.Command) protocol.CommandResult {
	r.calls <- c
	return protocol.CommandResult{CommandID: c.ID, State: "completed"}
}

func TestCollaborationCapabilityRejectsSilentDowngradeAndKeepsOriginalReceipt(t *testing.T) {
	h, s, server, token := testHub(t)
	runner := collaborationRunner{calls: make(chan protocol.Command, 4)}
	h.SetLocal(runner)
	device := protocol.Device{ID: "local", Projects: []protocol.Project{{ID: "project"}}}
	if err := h.Device(device); err != nil {
		t.Fatal(err)
	}
	bootstrap := request(t, server.URL, token, "GET", "/v1/bootstrap", nil)
	var snapshot protocol.NetworkSnapshot
	if err := json.NewDecoder(bootstrap.Body).Decode(&snapshot); err != nil || !slices.Contains(snapshot.Capabilities, "collaboration-modes") {
		t.Fatal("Hub did not advertise its ability to preserve collaboration policies", snapshot.Capabilities, err)
	}
	session := protocol.Session{ID: "existing", DeviceID: device.ID, ProjectID: "project", Policy: protocol.Policy{CollaborationMode: "plan"}}
	if err := s.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	version := "historic-plan"
	if err := s.Put("policies", session.ID+":"+version, protocol.PolicySnapshot{SessionID: session.ID, Version: version, Policy: session.Policy}); err != nil {
		t.Fatal(err)
	}
	post := func(c protocol.Command) protocol.CommandResult {
		t.Helper()
		response := request(t, server.URL, token, "POST", "/v1/commands", c)
		var result protocol.CommandResult
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	create := protocol.Command{ID: "unsupported-create", DeviceID: device.ID, ProjectID: "project", Kind: "create", Policy: &protocol.Policy{CollaborationMode: "plan"}}
	for _, command := range []protocol.Command{
		create,
		{ID: "inherited-start", DeviceID: device.ID, SessionID: session.ID, Kind: "start"},
		{ID: "historical-plan", DeviceID: device.ID, SessionID: session.ID, Kind: "policy.restore", RequestID: version},
	} {
		if result := post(command); result.State != "failed" || result.Error == "" {
			t.Fatal("an unsupported plan was dispatched as ordinary work", command.ID, result)
		}
	}
	// Stopping a retained planning session must remain available on an old node.
	post(protocol.Command{ID: "stop-old-plan", DeviceID: device.ID, SessionID: session.ID, Kind: "stop"})
	select {
	case command := <-runner.calls:
		if command.Kind != "stop" {
			t.Fatal("unsupported planning command reached the executor", command)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("capability check blocked a stop operation")
	}
	device.Capabilities = []string{"collaboration-modes"}
	if err := h.Device(device); err != nil {
		t.Fatal(err)
	}
	if result := post(create); result.State != "failed" {
		t.Fatal("capability change replayed the previously rejected command", result)
	}
	create.ID = "new-supported-plan"
	post(create)
	select {
	case command := <-runner.calls:
		if command.ID != create.ID || command.Policy.CollaborationMode != "plan" {
			t.Fatal("supported planning request changed in transit", command)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supported planning command was not dispatched")
	}
}
