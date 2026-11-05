package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"mewdex/internal/protocol"
)

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
