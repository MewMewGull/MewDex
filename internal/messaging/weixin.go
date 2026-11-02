package messaging

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"mewdex/internal/store"
)

const weixinBase = "https://ilinkai.weixin.qq.com"

type weixin struct {
	config channel
	store  *store.Store
	client *http.Client
	base   string
}

func newWeixin(c channel, s *store.Store) *weixin {
	base := c.BaseURL
	if base == "" {
		base = weixinBase
	}
	return &weixin{config: c, store: s, client: newHTTPClient(), base: strings.TrimRight(base, "/")}
}

func weixinHeaders(token string) http.Header {
	var random [4]byte
	_, _ = rand.Read(random[:])
	uin := strconv.FormatUint(uint64(binary.BigEndian.Uint32(random[:])), 10)
	headers := http.Header{"AuthorizationType": {"ilink_bot_token"}, "X-WECHAT-UIN": {base64.StdEncoding.EncodeToString([]byte(uin))}, "iLink-App-Id": {"bot"}, "iLink-App-ClientVersion": {"131584"}}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	return headers
}

func (w *weixin) post(ctx context.Context, endpoint string, payload map[string]any, out any) error {
	payload["base_info"] = map[string]string{"channel_version": "2.2.0"}
	return platformJSON(ctx, w.client, "POST", w.base+"/ilink/bot/"+endpoint, weixinHeaders(w.config.Secret), payload, out)
}

type weixinMessage struct {
	ID           json.RawMessage `json:"message_id"`
	From         string          `json:"from_user_id"`
	MessageType  int             `json:"message_type"`
	ContextToken string          `json:"context_token"`
	Items        []struct {
		Type int `json:"type"`
		Text struct {
			Text string `json:"text"`
		} `json:"text_item"`
		Voice struct {
			Text string `json:"text"`
		} `json:"voice_item"`
	} `json:"item_list"`
}

func (w *weixin) Run(ctx context.Context, receive func(Envelope) error, status func(string, string)) error {
	var cursor string
	if err := w.store.Get("messaging_weixin_cursor", w.config.AccountID, &cursor); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	connected := false
	for ctx.Err() == nil {
		var response struct {
			Ret     int             `json:"ret"`
			Errcode int             `json:"errcode"`
			Cursor  string          `json:"get_updates_buf"`
			Msgs    []weixinMessage `json:"msgs"`
		}
		if err := w.post(ctx, "getupdates", map[string]any{"get_updates_buf": cursor}, &response); err != nil {
			return err
		}
		if response.Ret != 0 || response.Errcode != 0 {
			return errors.New("微信登录状态或轮询不可用")
		}
		if !connected {
			status("connected", "微信 iLink 已连接；仅处理允许列表中的用户")
			connected = true
		}
		for _, message := range response.Msgs {
			if message.From == "" || message.From == w.config.AccountID || message.MessageType == 2 {
				continue
			}
			id := string(message.ID)
			var stringID string
			if json.Unmarshal(message.ID, &stringID) == nil {
				id = stringID
			}
			if id == "" || id == "null" {
				continue
			}
			parts := []string{}
			for _, item := range message.Items {
				if item.Type == 1 && item.Text.Text != "" {
					parts = append(parts, item.Text.Text)
				} else if item.Type == 3 && item.Voice.Text != "" {
					parts = append(parts, item.Voice.Text)
				}
			}
			if message.ContextToken != "" {
				if err := w.store.Put("messaging_weixin_peers", stable(w.config.AccountID, message.From), message.ContextToken); err != nil {
					return err
				}
			}
			e := Envelope{ID: id, Platform: "weixin", Account: w.config.AccountID, UserID: message.From, ChatID: message.From, Scene: "c2c", Text: strings.Join(parts, "\n"), ReplyToken: message.ContextToken}
			if err := receive(e); err != nil {
				return err
			}
		}
		// Commit the platform cursor only after every message is durably accepted.
		if response.Cursor != "" && response.Cursor != cursor {
			if err := w.store.Put("messaging_weixin_cursor", w.config.AccountID, response.Cursor); err != nil {
				return err
			}
			cursor = response.Cursor
		}
	}
	return ctx.Err()
}

func (w *weixin) Send(ctx context.Context, e Envelope, text, id string) error {
	contextToken := e.ReplyToken
	var latest string
	if w.store.Get("messaging_weixin_peers", stable(w.config.AccountID, e.UserID), &latest) == nil && latest != "" {
		contextToken = latest
	}
	if contextToken == "" {
		return errors.New("微信回复缺少当前用户的会话令牌")
	}
	for index, chunk := range SplitText(text, 3500) {
		message := map[string]any{"from_user_id": "", "to_user_id": e.UserID, "client_id": stable(id, strconv.Itoa(index)), "message_type": 2, "message_state": 2, "context_token": contextToken, "item_list": []any{map[string]any{"type": 1, "text_item": map[string]string{"text": chunk}}}}
		var response struct {
			Ret     int `json:"ret"`
			Errcode int `json:"errcode"`
		}
		if err := w.post(ctx, "sendmessage", map[string]any{"msg": message}, &response); err != nil {
			return err
		}
		if response.Ret != 0 || response.Errcode != 0 {
			return fmt.Errorf("微信回复失败（%d/%d），请检查账号连接", response.Ret, response.Errcode)
		}
	}
	return nil
}
