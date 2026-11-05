package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mewdex/internal/protocol"
	"net/http"
)

// Reuse the exact command entry point used by first-party inputs, including
// scope checks, approval restrictions, durable claims and remote dispatch.
func (h *Hub) submitMessagingCommand(ctx context.Context, identityID string, command protocol.Command) (protocol.CommandResult, error) {
	id, err := h.store.Identity(identityID)
	if err != nil || id.Role != "client" {
		return protocol.CommandResult{}, errors.New("消息输入身份已撤销")
	}
	request, err := http.NewRequestWithContext(context.WithValue(ctx, identityKey{}, id), "POST", "/v1/commands", bytes.NewReader(protocol.JSON(command)))
	if err != nil {
		return protocol.CommandResult{}, err
	}
	response := &commandResponse{header: http.Header{}}
	h.command(response, request, id)
	var result protocol.CommandResult
	if response.status >= 400 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(response.body.Bytes(), &problem)
		if problem.Error == "" {
			problem.Error = "Hub 未接受此消息指令"
		}
		return result, errors.New(problem.Error)
	}
	err = json.Unmarshal(response.body.Bytes(), &result)
	return result, err
}

type commandResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *commandResponse) Header() http.Header         { return r.header }
func (r *commandResponse) WriteHeader(status int)      { r.status = status }
func (r *commandResponse) Write(b []byte) (int, error) { return r.body.Write(b) }
