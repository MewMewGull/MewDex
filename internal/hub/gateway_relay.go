package hub

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"sync"

	"github.com/coder/websocket"
	"mewdex/internal/protocol"
)

type gatewayRelayResult struct {
	connection net.Conn
	err        error
}

type gatewayRelay struct {
	provider *peer
	ctx      context.Context
	result   chan gatewayRelayResult
}

// OpenGateway requests an outbound data connection from the configured node.
// Both connections authenticate through the existing Hub and node credentials.
func (h *Hub) OpenGateway(ctx context.Context, target string) (net.Conn, error) {
	if h.config.Gateway == nil || h.config.Gateway.ForwardDeviceID == "" {
		return nil, errors.New("a gateway device is not configured")
	}
	id := protocol.ID()
	h.mu.Lock()
	provider := h.peers[h.config.Gateway.ForwardDeviceID]
	if provider == nil || !slices.Contains(provider.device.Capabilities, "model-gateway-relay") || !slices.Contains(provider.device.Roles, "gateway") {
		h.mu.Unlock()
		return nil, errors.New("the configured gateway device is offline or needs an update")
	}
	relay := &gatewayRelay{provider: provider, ctx: ctx, result: make(chan gatewayRelayResult)}
	h.gatewayRelays[id] = relay
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.gatewayRelays, id)
		h.mu.Unlock()
	}()
	request := protocol.GatewayTunnelRequest{ID: id, Target: target}
	if err := provider.send(ctx, protocol.Frame{Version: protocol.Version, Type: "gateway.open", ID: id, Payload: protocol.JSON(request)}); err != nil {
		return nil, errors.New("gateway device request failed")
	}
	select {
	case result := <-relay.result:
		return result.connection, result.err
	case <-provider.ctx.Done():
		return nil, errors.New("gateway device disconnected")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *Hub) failGatewayRelay(provider *peer, id string) {
	h.mu.Lock()
	relay := h.gatewayRelays[id]
	if relay == nil || relay.provider != provider {
		h.mu.Unlock()
		return
	}
	delete(h.gatewayRelays, id)
	h.mu.Unlock()
	select {
	case relay.result <- gatewayRelayResult{err: errors.New("gateway device could not open the model connection")}:
	case <-relay.ctx.Done():
	case <-provider.ctx.Done():
	}
}

func (h *Hub) acceptGatewayRelay(w http.ResponseWriter, r *http.Request, identity protocol.Identity) {
	id := r.URL.Query().Get("id")
	h.mu.Lock()
	relay := h.gatewayRelays[id]
	if identity.Role != "executor" || relay == nil || relay.provider.identityID != identity.ID || relay.provider.device.ID != identity.DeviceID || h.peers[identity.DeviceID] != relay.provider {
		h.mu.Unlock()
		problem(w, http.StatusForbidden, errors.New("gateway relay does not belong to this node"))
		return
	}
	delete(h.gatewayRelays, id)
	h.mu.Unlock()
	socket, err := websocket.Accept(w, r, nil)
	if err != nil {
		select {
		case relay.result <- gatewayRelayResult{err: errors.New("gateway data handshake failed")}:
		case <-relay.ctx.Done():
		}
		return
	}
	connection := &gatewayRelayConnection{Conn: websocket.NetConn(r.Context(), socket, websocket.MessageBinary), socket: socket, done: make(chan struct{})}
	defer connection.Close()
	select {
	case relay.result <- gatewayRelayResult{connection: connection}:
	case <-relay.ctx.Done():
		return
	case <-r.Context().Done():
		return
	}
	select {
	case <-connection.done:
	case <-relay.provider.ctx.Done():
	case <-r.Context().Done():
	}
}

type gatewayRelayConnection struct {
	net.Conn
	socket *websocket.Conn
	done   chan struct{}
	once   sync.Once
}

func (c *gatewayRelayConnection) Close() error {
	var err error
	c.once.Do(func() {
		err = c.socket.CloseNow()
		close(c.done)
	})
	return err
}
