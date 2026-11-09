package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"mewdex/internal/protocol"
)

// Handle is shared by the Hub-hosted connector and authenticated remote bridges.
// The caller binds Owner to the administrator who made the management request.
func (m *Manager) Handle(ctx context.Context, request protocol.InputBridgeRequest) protocol.InputBridgeResponse {
	var value any
	var err error
	path := strings.TrimPrefix(request.Path, "/v1/messaging")
	switch {
	case request.Method == "GET" && path == "":
		value = m.Snapshot()
	case request.Method == "POST" && path == "/weixin/login":
		value, err = m.BeginWeixinLogin(ctx, request.Owner)
	case request.Method == "POST" && path == "/weixin/login/poll":
		value, err = m.PollWeixinLogin(ctx, request.Owner)
	case request.Method == "PUT" && strings.Count(path, "/") == 1:
		var update protocol.MessagingUpdate
		err = json.Unmarshal(request.Body, &update)
		if err == nil {
			err = m.Update(strings.TrimPrefix(path, "/"), update)
		}
		value = m.Snapshot()
	default:
		err = errors.New("unknown message channel operation")
	}
	status := 200
	if err != nil {
		status, value = 400, map[string]string{"error": err.Error()}
	}
	return protocol.InputBridgeResponse{ID: request.ID, Status: status, Body: protocol.JSON(value)}
}
