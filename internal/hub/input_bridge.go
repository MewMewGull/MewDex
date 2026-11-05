package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/protocol"
)

type inputBridge struct {
	view    protocol.InputBridge
	owner   string
	conn    *websocket.Conn
	write   sync.Mutex
	mu      sync.Mutex
	pending map[string]chan protocol.InputBridgeResponse
	done    chan struct{}
}

func (h *Hub) inputBridgeWS(w http.ResponseWriter, r *http.Request) {
	actor := identity(r)
	id, name := r.URL.Query().Get("id"), r.URL.Query().Get("name")
	if actor.Role != "admin" {
		problem(w, 403, errors.New("admin required"))
		return
	}
	if len(id) < 16 || len(id) > 128 || name == "" || len(name) > 256 {
		problem(w, 400, errors.New("invalid APP bridge identity"))
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(2 << 20)
	b := &inputBridge{view: protocol.InputBridge{ID: id, Name: name}, owner: actor.ID, conn: conn, pending: map[string]chan protocol.InputBridgeResponse{}, done: make(chan struct{})}
	key := actor.ID + "/" + id
	h.mu.Lock()
	if h.inputBridges == nil {
		h.inputBridges = map[string]*inputBridge{}
	}
	old := h.inputBridges[key]
	h.inputBridges[key] = b
	h.mu.Unlock()
	if old != nil {
		old.conn.CloseNow()
	}
	defer func() {
		close(b.done)
		h.mu.Lock()
		if h.inputBridges[key] == b {
			delete(h.inputBridges, key)
		}
		h.mu.Unlock()
	}()
	for {
		_, raw, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var response protocol.InputBridgeResponse
		if json.Unmarshal(raw, &response) != nil || response.ID == "" || response.Status < 200 || response.Status > 599 || !json.Valid(response.Body) {
			return
		}
		b.mu.Lock()
		pending := b.pending[response.ID]
		b.mu.Unlock()
		if pending != nil {
			select {
			case pending <- response:
			default:
			}
		}
	}
}

func (h *Hub) listInputBridges(w http.ResponseWriter, r *http.Request) {
	actor := identity(r)
	if actor.Role != "admin" {
		problem(w, 403, errors.New("admin required"))
		return
	}
	views := []protocol.InputBridge{}
	h.mu.Lock()
	for _, b := range h.inputBridges {
		if b.owner == actor.ID {
			views = append(views, b.view)
		}
	}
	h.mu.Unlock()
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	reply(w, 200, views)
}

func (h *Hub) relayMessaging(w http.ResponseWriter, r *http.Request) {
	actor, selected := identity(r), r.URL.Query().Get("bridgeId")
	h.mu.Lock()
	var target *inputBridge
	count := 0
	for _, b := range h.inputBridges {
		if b.owner == actor.ID && (selected == "" || selected == b.view.ID) {
			target = b
			count++
		}
	}
	h.mu.Unlock()
	if count == 0 {
		problem(w, 503, errors.New("请打开已登录此账号的 APP，由 APP 连接消息渠道"))
		return
	}
	if count > 1 {
		problem(w, 409, errors.New("请选择负责消息渠道的 APP"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		problem(w, 400, errors.New("channel request is too large"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	request := protocol.InputBridgeRequest{ID: protocol.ID(), Method: r.Method, Path: r.URL.Path, Owner: actor.ID, Body: body}
	result := make(chan protocol.InputBridgeResponse, 1)
	target.mu.Lock()
	if len(target.pending) >= 16 {
		target.mu.Unlock()
		problem(w, 429, errors.New("APP 渠道请求繁忙"))
		return
	}
	target.pending[request.ID] = result
	target.mu.Unlock()
	defer func() { target.mu.Lock(); delete(target.pending, request.ID); target.mu.Unlock() }()
	target.write.Lock()
	err = target.conn.Write(ctx, websocket.MessageText, protocol.JSON(request))
	target.write.Unlock()
	if err != nil {
		problem(w, 503, errors.New("APP 渠道连接已中断"))
		return
	}
	select {
	case response := <-result:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(response.Status)
		_, _ = w.Write(response.Body)
	case <-target.done:
		problem(w, 503, errors.New("APP 渠道连接已中断"))
	case <-ctx.Done():
		problem(w, 504, errors.New("APP 渠道操作结果待确认，请刷新渠道状态"))
	}
}
