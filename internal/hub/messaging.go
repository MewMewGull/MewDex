package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"mewdex/internal/messaging"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

const hubMessagingBridgeID = "hub-message-connector"

func (h *Hub) StartMessaging(ctx context.Context, channels *store.Store) (*messaging.Manager, error) {
	m, err := messaging.NewForHub(channels, h.store, h.submitMessagingCommand)
	if err != nil {
		return nil, err
	}
	m.Start(ctx)
	h.mu.Lock()
	h.messaging = m
	h.mu.Unlock()
	return m, nil
}

func (h *Hub) messagingAPI(w http.ResponseWriter, r *http.Request) {
	if identity(r).Role != "admin" {
		problem(w, 403, errors.New("只有管理员可以管理消息渠道"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/messaging")
	if r.Method == "PUT" && strings.Count(path, "/") == 1 && h.coordinator != nil {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		var update protocol.MessagingUpdate
		if err != nil || json.Unmarshal(body, &update) != nil {
			problem(w, 400, errors.New("invalid channel settings"))
			return
		}
		if update.Enabled && update.DeviceID != h.coordinatorID {
			problem(w, 400, errors.New("请选择由 Hub 管理的项目"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	h.relayMessaging(w, r)
}
