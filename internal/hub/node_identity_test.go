package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestNodeIdentityRejectsDuplicateProcessAndAllowsSameProcessReconnect(t *testing.T) {
	h, state, server, _ := testHub(t)
	h.config.HostID = store.Hash("hub-host")
	code, _ := state.Invite("executor", time.Minute)
	token, _, err := state.PairWithHost(code, "Mac", "mac", store.Hash("mac-host"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(instance, host, name string) (*websocket.Conn, protocol.Frame) {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, strings.Replace(server.URL, "http", "ws", 1)+"/v1/node/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		device := protocol.Device{ID: "mac", HostID: host, Name: name, Roles: []string{"executor"}, Projects: []protocol.Project{}}
		if err = conn.Write(ctx, websocket.MessageText, protocol.JSON(protocol.Frame{Version: protocol.Version, Type: "hello", ID: instance, Payload: protocol.JSON(device)})); err != nil {
			t.Fatal(err)
		}
		_, raw, err := conn.Read(ctx)
		var response protocol.Frame
		if err != nil || json.Unmarshal(raw, &response) != nil {
			t.Fatal("node handshake failed", err)
		}
		return conn, response
	}
	first, accepted := dial("process-one", store.Hash("mac-host"), "current Mac")
	if accepted.Type != "hello.accepted" {
		t.Fatal("valid executor rejected", string(accepted.Payload))
	}
	if _, rejected := dial("process-two", store.Hash("mac-host"), "duplicate Mac"); rejected.Type != "error" {
		t.Fatal("a second live execution process replaced the current one")
	}
	if _, rejected := dial("process-one", store.Hash("other-host"), "copied credential"); rejected.Type != "error" {
		t.Fatal("a copied credential moved to another host")
	}
	if err = first.Write(ctx, websocket.MessageText, protocol.JSON(protocol.Frame{Version: protocol.Version, Type: "heartbeat"})); err != nil {
		t.Fatal(err)
	}
	if _, _, err = first.Read(ctx); err != nil {
		t.Fatal("rejected duplicate interrupted the valid connection", err)
	}
	h.mu.Lock()
	previous := h.peers["mac"]
	h.mu.Unlock()
	if _, accepted = dial("process-one", store.Hash("mac-host"), "reconnected Mac"); accepted.Type != "hello.accepted" {
		t.Fatal("the same process could not reconnect")
	}
	if _, err = h.nodeFrame(previous, protocol.Frame{Version: protocol.Version, Type: "heartbeat"}); err == nil {
		t.Fatal("old socket overwrote the new device metadata")
	}
	event := protocol.Event{ID: "stale-event", DeviceID: "mac", Kind: "session.updated", Payload: protocol.JSON(protocol.Session{ID: "stale-session", DeviceID: "mac"})}
	if _, err = h.nodeFrame(previous, protocol.Frame{Version: protocol.Version, Type: "event", Payload: protocol.JSON(event)}); err == nil {
		t.Fatal("old socket published events after replacement")
	}
	var device protocol.Device
	if err = state.Get("devices", "mac", &device); err != nil || device.Name != "reconnected Mac" {
		t.Fatal("stale connection changed current device metadata", err)
	}
}
