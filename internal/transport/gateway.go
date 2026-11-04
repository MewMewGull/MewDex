package transport

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/protocol"
)

func (n *Node) relayGateway(ctx context.Context, request protocol.GatewayTunnelRequest, token string, send func(protocol.Frame) error) {
	fail := func() { _ = send(protocol.Frame{Version: protocol.Version, Type: "gateway.error", ID: request.ID}) }
	if n.Gateway == nil {
		fail()
		return
	}
	opening, cancel := context.WithTimeout(ctx, 15*time.Second)
	upstream, err := n.Gateway.Open(opening, request.Target)
	cancel()
	if err != nil {
		fail()
		return
	}
	defer upstream.Close()
	endpoint, _ := url.Parse(n.Config.HubURL)
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/v1/gateway/relay/ws"
	query := endpoint.Query()
	query.Set("id", request.ID)
	endpoint.RawQuery = query.Encode()
	opening, cancel = context.WithTimeout(ctx, 15*time.Second)
	socket, _, err := websocket.Dial(opening, endpoint.String(), &websocket.DialOptions{HTTPClient: hubHTTP, HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	cancel()
	if err != nil {
		fail()
		return
	}
	defer socket.CloseNow()
	stream := websocket.NetConn(ctx, socket, websocket.MessageBinary)
	stop := context.AfterFunc(ctx, func() {
		_ = upstream.Close()
		_ = socket.CloseNow()
	})
	defer stop()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, stream)
		_ = upstream.Close()
		_ = socket.CloseNow()
		close(done)
	}()
	_, _ = io.Copy(stream, upstream)
	_ = socket.CloseNow()
	_ = upstream.Close()
	<-done
}
