package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"mewdex/internal/messaging"
	"mewdex/internal/protocol"
)

func (h *Hub) StartMessaging(ctx context.Context) (func(), error) {
	manager, err := messaging.New(h.store, h.submitMessagingCommand)
	if err != nil {
		return nil, err
	}
	manager.Start(ctx)
	h.mu.Lock()
	h.messageChannels = manager
	h.mu.Unlock()
	return manager.Close, nil
}

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

func (h *Hub) messagingAPI(w http.ResponseWriter, r *http.Request) {
	if identity(r).Role != "admin" {
		problem(w, 403, errors.New("只有管理员可以管理消息渠道"))
		return
	}
	h.mu.Lock()
	manager := h.messageChannels
	h.mu.Unlock()
	if manager == nil {
		problem(w, 503, errors.New("消息渠道尚未启动"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/messaging")
	if r.Method == "GET" && path == "" {
		reply(w, 200, manager.Snapshot())
		return
	}
	if r.Method == "POST" && (path == "/weixin/login" || path == "/weixin/login/poll") {
		var result protocol.MessagingLogin
		var err error
		if path == "/weixin/login" {
			result, err = manager.BeginWeixinLogin(r.Context(), identity(r).ID)
		} else {
			result, err = manager.PollWeixinLogin(r.Context(), identity(r).ID)
		}
		if err != nil {
			problem(w, 400, err)
			return
		}
		reply(w, 200, result)
		return
	}
	if r.Method == "PUT" && strings.Count(path, "/") == 1 {
		var update protocol.MessagingUpdate
		if !decode(w, r, &update) {
			return
		}
		if err := manager.Update(strings.TrimPrefix(path, "/"), update); err != nil {
			problem(w, 400, err)
			return
		}
		reply(w, 200, manager.Snapshot())
		return
	}
	problem(w, 404, errors.New("消息渠道接口不存在"))
}
