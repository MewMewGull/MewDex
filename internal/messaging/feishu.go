package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"mewdex/internal/protocol"
)

type feishu struct {
	config  channel
	client  *http.Client
	base    string
	mu      sync.Mutex
	token   string
	expires time.Time
	botID   string
}

func newFeishu(c channel) *feishu {
	return &feishu{config: c, client: newHTTPClient(), base: "https://open.feishu.cn", botID: c.BotUserID}
}

func (f *feishu) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Until(f.expires) > time.Minute {
		return f.token, nil
	}
	var response struct {
		Code    int    `json:"code"`
		Token   string `json:"tenant_access_token"`
		Expires int    `json:"expire"`
	}
	if err := platformJSON(ctx, f.client, "POST", f.base+"/open-apis/auth/v3/tenant_access_token/internal", nil, map[string]string{"app_id": f.config.AppID, "app_secret": f.config.Secret}, &response); err != nil {
		return "", err
	}
	if response.Code != 0 || response.Token == "" || response.Expires < 1 {
		return "", errors.New("飞书未返回有效的应用凭据")
	}
	f.token, f.expires = response.Token, time.Now().Add(time.Duration(min(response.Expires, 86400))*time.Second)
	return f.token, nil
}

func (f *feishu) api(ctx context.Context, method, path string, body any, response any) error {
	token, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	err = platformJSON(ctx, f.client, method, f.base+path, http.Header{"Authorization": {"Bearer " + token}}, body, response)
	var failure *platformError
	if errors.As(err, &failure) && failure.Status == http.StatusUnauthorized {
		f.mu.Lock()
		f.token = ""
		f.mu.Unlock()
	}
	return err
}

// The SDK can log request URLs and raw event bodies. MewDex exposes only its
// sanitized lifecycle callbacks and keeps credentials and chat content private.
type quietLarkLogger struct{}

func (quietLarkLogger) Debug(context.Context, ...interface{}) {}
func (quietLarkLogger) Info(context.Context, ...interface{})  {}
func (quietLarkLogger) Warn(context.Context, ...interface{})  {}
func (quietLarkLogger) Error(context.Context, ...interface{}) {}

func (f *feishu) Run(ctx context.Context, receive func(Envelope) error, status func(string, string)) error {
	var info struct {
		Code int `json:"code"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := f.api(ctx, "GET", "/open-apis/bot/v3/info", nil, &info); err != nil {
		return err
	}
	if info.Code != 0 || info.Bot.OpenID == "" || f.config.BotUserID != "" && f.config.BotUserID != info.Bot.OpenID {
		return errors.New("飞书机器人身份不可用，请检查机器人能力和 Open ID")
	}
	f.mu.Lock()
	f.botID = info.Bot.OpenID
	f.mu.Unlock()
	handler := dispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		if event == nil {
			return nil
		}
		if e, ok := f.decode(event.Event); ok {
			return receive(e) // ACK only after the shared durable inbox succeeds.
		}
		return nil
	})
	handler.Config.Logger = quietLarkLogger{}
	client := larkws.NewClient(f.config.AppID, f.config.Secret,
		larkws.WithEventHandler(handler), larkws.WithLogger(quietLarkLogger{}),
		larkws.WithHttpClient(f.client), larkws.WithDomain(f.base),
		larkws.WithOnReady(func() { status("connected", "飞书长连接已建立，群聊需要 @机器人") }),
		larkws.WithOnReconnecting(func() { status("retrying", "飞书长连接正在重连") }),
		larkws.WithOnReconnected(func() { status("connected", "飞书长连接已恢复") }),
		larkws.WithOnError(func(error) { status("retrying", "飞书连接异常，请检查凭据、消息事件订阅和网络") }))
	defer client.Close()
	return client.Start(ctx)
}

func ptrText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (f *feishu) decode(data *larkim.P2MessageReceiveV1Data) (Envelope, bool) {
	if data == nil || data.Message == nil || data.Sender == nil || data.Sender.SenderId == nil || ptrText(data.Sender.SenderType) != "user" {
		return Envelope{}, false
	}
	message := data.Message
	e := Envelope{ID: ptrText(message.MessageId), Platform: "feishu", Account: f.config.AppID, ChatID: ptrText(message.ChatId), UserID: ptrText(data.Sender.SenderId.OpenId), ThreadID: ptrText(message.ThreadId), Scene: "p2p", ReplyToken: ptrText(message.MessageId)}
	e.Group = ptrText(message.ChatType) == "group"
	if e.Group {
		e.Scene = "group"
	}
	f.mu.Lock()
	botID := f.botID
	f.mu.Unlock()
	if ptrText(message.MessageType) == "text" {
		var content struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(ptrText(message.Content)), &content) == nil {
			e.Text = content.Text
		}
	} else if ptrText(message.MessageType) == "post" {
		e.Text = feishuPostText(ptrText(message.Content))
	}
	for _, mention := range message.Mentions {
		if mention != nil && mention.Id != nil && botID != "" && ptrText(mention.Id.OpenId) == botID {
			e.Mentioned = true
			e.Text = strings.ReplaceAll(e.Text, ptrText(mention.Key), "")
		}
	}
	e.Text = strings.TrimSpace(e.Text)
	return e, e.ID != "" && e.ChatID != "" && e.UserID != ""
}

func feishuPostText(raw string) string {
	var post map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &post) != nil {
		return ""
	}
	if _, hasContent := post["content"]; !hasContent {
		for _, locale := range []string{"zh_cn", "en_us", "ja_jp"} {
			if content, exists := post[locale]; exists {
				return feishuPostText(string(content))
			}
		}
	}
	var title string
	_ = json.Unmarshal(post["title"], &title)
	var rows [][]struct {
		Tag  string `json:"tag"`
		Text string `json:"text"`
		Href string `json:"href"`
	}
	if json.Unmarshal(post["content"], &rows) != nil {
		return title
	}
	lines := []string{}
	if title != "" {
		lines = append(lines, title)
	}
	for _, row := range rows {
		var line strings.Builder
		for _, item := range row {
			if item.Tag == "text" || item.Tag == "md" || item.Tag == "a" {
				line.WriteString(item.Text)
				if item.Tag == "a" && item.Href != "" {
					line.WriteString(" (" + item.Href + ")")
				}
			}
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func (f *feishu) Send(ctx context.Context, e Envelope, text, id string) error {
	for index, chunk := range SplitText(text, 12000) {
		content := map[string]any{"zh_cn": map[string]any{"title": "", "content": []any{[]any{map[string]string{"tag": "md", "text": chunk}}}}}
		body := map[string]any{"msg_type": "post", "content": string(protocol.JSON(content)), "uuid": stable(id, strconv.Itoa(index)), "reply_in_thread": e.ThreadID != ""}
		var response struct {
			Code int `json:"code"`
		}
		if err := f.api(ctx, "POST", "/open-apis/im/v1/messages/"+url.PathEscape(e.ID)+"/reply", body, &response); err != nil {
			return err
		}
		if response.Code != 0 {
			return fmt.Errorf("飞书回复失败（%d）", response.Code)
		}
	}
	return nil
}
