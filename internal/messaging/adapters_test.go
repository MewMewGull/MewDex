package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestWeixinPersistsIntakeBeforeCursorAndUsesLatestReplyContext(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	polls := 0
	var sent []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bot-secret" || r.Header.Get("AuthorizationType") != "ilink_bot_token" || r.Header.Get("X-WECHAT-UIN") == "" {
			t.Error("missing iLink authentication headers")
		}
		if strings.HasSuffix(r.URL.Path, "getupdates") {
			polls++
			if polls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "get_updates_buf": "next", "msgs": []any{map[string]any{"message_id": 42, "from_user_id": "peer", "message_type": 1, "context_token": "latest", "item_list": []any{map[string]any{"type": 1, "text_item": map[string]string{"text": "你好"}}}}}})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"ret": -14})
			}
			return
		}
		var payload struct {
			Message map[string]any `json:"msg"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			t.Error("invalid outbound JSON")
		}
		sent = append(sent, payload.Message)
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()
	w := newWeixin(channel{Platform: "weixin", AccountID: "account", Secret: "bot-secret"}, s)
	w.base = server.URL
	var received Envelope
	err = w.Run(context.Background(), func(e Envelope) error {
		received = e
		var cursor string
		if s.Get("messaging_weixin_cursor", "account", &cursor) == nil {
			t.Error("platform cursor advanced before durable intake")
		}
		return nil
	}, func(string, string) {})
	if err == nil || received.ID != "42" || received.Text != "你好" {
		t.Fatalf("incorrect iLink envelope: %#v / %v", received, err)
	}
	var cursor string
	if s.Get("messaging_weixin_cursor", "account", &cursor) != nil || cursor != "next" {
		t.Fatal("accepted cursor was not persisted")
	}
	received.ReplyToken = "stale"
	text := strings.Repeat("中文🙂", 800)
	for i := 0; i < 2; i++ {
		if err = w.Send(context.Background(), received, text, "delivery"); err != nil {
			t.Fatal(err)
		}
	}
	if len(sent) < 4 || len(sent)%2 != 0 {
		t.Fatal("long UTF-8 reply was not chunked")
	}
	for i, message := range sent[:len(sent)/2] {
		if message["context_token"] != "latest" || message["to_user_id"] != "peer" || message["client_id"] != sent[i+len(sent)/2]["client_id"] {
			t.Fatal("reply context, recipient or retry idempotency changed")
		}
	}
}

func TestWeixinFailedIntakeDoesNotAdvancePlatformCursor(t *testing.T) {
	m, _, _ := testManager(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"unsafe","msgs":[{"message_id":"id","from_user_id":"peer","context_token":"context","item_list":[]}]}`))
	}))
	defer server.Close()
	w := newWeixin(channel{AccountID: "account"}, m.store)
	w.base = server.URL
	err := w.Run(context.Background(), func(Envelope) error { return errors.New("storage failed") }, func(string, string) {})
	var cursor string
	if err == nil || !errors.Is(m.store.Get("messaging_weixin_cursor", "account", &cursor), store.ErrNotFound) {
		t.Fatal("unaccepted messages were skipped")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestWeixinQRLoginIsOwnerScopedAndKeepsDisabledChannelDisabled(t *testing.T) {
	m, _, _ := testManager(t)
	m.loginClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"qrcode":"opaque-login","qrcode_img_content":"https://liteapp.weixin.qq.com/example"}`
		if strings.HasSuffix(r.URL.Path, "get_qrcode_status") {
			body = `{"status":"confirmed","bot_token":"private-token","ilink_bot_id":"wx-account","ilink_user_id":"scanning-user","baseurl":"https://ilinkai.weixin.qq.com"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	login, err := m.BeginWeixinLogin(context.Background(), "owner")
	if err != nil || login.State != "wait" || login.URL == "" {
		t.Fatal("QR was not created")
	}
	if _, err = m.PollWeixinLogin(context.Background(), "another-owner"); err == nil {
		t.Fatal("another identity claimed this QR login")
	}
	login, err = m.PollWeixinLogin(context.Background(), "owner")
	c, _ := m.config("weixin")
	if err != nil || login.State != "confirmed" || c.Enabled || c.Secret != "private-token" || len(c.AllowedUsers) != 1 || c.AllowedUsers[0] != "scanning-user" {
		t.Fatalf("login granted unintended access or lost credentials: %#v / %v", login, err)
	}
	if strings.Contains(string(protocol.JSON(m.Snapshot())), "private-token") {
		t.Fatal("QR token leaked through channel snapshot")
	}
	for _, raw := range []string{"http://ilinkai.weixin.qq.com", "https://weixin.qq.com.evil.example", "https://ilinkai.weixin.qq.com@evil.example", "https://ilinkai.weixin.qq.com/path", "https://ilinkai.weixin.qq.com?token=abc", "https://ilinkai.weixin.qq.com:8443"} {
		if _, err := trustedWeixinBase(raw); err == nil {
			t.Errorf("untrusted credential destination accepted: %s", raw)
		}
	}
}

func TestQQWebsocketResumesFromDurablyReceivedSequence(t *testing.T) {
	m, _, _ := testManager(t)
	var server *httptest.Server
	var mu sync.Mutex
	var authFrames []map[string]json.RawMessage
	var replyBodies []map[string]any
	connections := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"qq-token","expires_in":"7200"}`))
		case "/gateway":
			_ = json.NewEncoder(w).Encode(map[string]string{"url": strings.Replace(server.URL, "http://", "ws://", 1) + "/ws"})
		case "/ws":
			connection, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer connection.CloseNow()
			ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			defer cancel()
			_ = connection.Write(ctx, websocket.MessageText, []byte(`{"op":10,"d":{"heartbeat_interval":1000}}`))
			_, data, err := connection.Read(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			var auth map[string]json.RawMessage
			_ = json.Unmarshal(data, &auth)
			mu.Lock()
			authFrames = append(authFrames, auth)
			connections++
			index := connections
			mu.Unlock()
			if index == 1 {
				_ = connection.Write(ctx, websocket.MessageText, []byte(`{"op":0,"s":1,"t":"READY","d":{"session_id":"session","user":{"id":"bot"}}}`))
			} else {
				_ = connection.Write(ctx, websocket.MessageText, []byte(`{"op":0,"s":2,"t":"RESUMED","d":{}}`))
			}
			_ = connection.Write(ctx, websocket.MessageText, []byte(`{"op":0,"s":3,"t":"C2C_MESSAGE_CREATE","d":{"id":"message","content":"hello","author":{"user_openid":"alice"}}}`))
			_ = connection.Write(ctx, websocket.MessageText, []byte(`{"op":7,"d":{}}`))
			_, _, _ = connection.Read(ctx)
		default:
			if r.Header.Get("Authorization") != "QQBot qq-token" || r.Header.Get("X-Union-Appid") != "app" || r.URL.Path != "/v2/users/alice/messages" {
				t.Error("incorrect QQ outbound auth or recipient")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			replyBodies = append(replyBodies, body)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"sent"}`))
		}
	}))
	defer server.Close()
	q := newQQ(channel{AppID: "app", Secret: "qq-secret"}, m.store)
	q.base, q.tokenURL = server.URL, server.URL+"/token"
	for index := 0; index < 2; index++ {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		err := q.Run(ctx, func(e Envelope) error {
			if e.UserID != "alice" || e.Scene != "c2c" {
				t.Error("QQ event was not normalized")
			}
			return nil
		}, func(string, string) {})
		cancel()
		if err == nil {
			t.Fatal("server reconnect was ignored")
		}
	}
	mu.Lock()
	frames := append([]map[string]json.RawMessage{}, authFrames...)
	mu.Unlock()
	if len(frames) != 2 || string(frames[0]["op"]) != "2" || string(frames[1]["op"]) != "6" || !strings.Contains(string(frames[1]["d"]), `"seq":3`) {
		t.Fatalf("QQ did not resume from committed sequence: %#v", frames)
	}
	var identify struct {
		Intents int `json:"intents"`
	}
	if json.Unmarshal(frames[0]["d"], &identify) != nil || identify.Intents != 1<<25 {
		t.Fatal("QQ requested extra intents that can reject C2C/group-only apps")
	}
	e := inbound("message", "alice")
	for index := 0; index < 2; index++ {
		if err := q.Send(context.Background(), e, "完成", "same-delivery"); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	bodies := append([]map[string]any{}, replyBodies...)
	mu.Unlock()
	if len(bodies) != 2 || bodies[0]["msg_seq"] != bodies[1]["msg_seq"] || bodies[0]["msg_id"] != "message" {
		t.Fatal("QQ retries did not retain their reply identity")
	}
}

func TestFeishuOnlyRecognizesOwnMentionAndSendsMarkdownInSourceThread(t *testing.T) {
	f := newFeishu(channel{AppID: "cli_app", Secret: "feishu-secret", BotUserID: "ou_bot"})
	var event larkim.P2MessageReceiveV1
	if err := json.Unmarshal([]byte(`{"event":{"sender":{"sender_type":"user","sender_id":{"open_id":"ou_alice"}},"message":{"message_id":"om_source","chat_id":"oc_group","chat_type":"group","thread_id":"omt_thread","message_type":"text","content":"{\"text\":\"@_user_1 hello\"}","mentions":[{"key":"@_user_1","id":{"open_id":"ou_other"}}]}}}`), &event); err != nil {
		t.Fatal(err)
	}
	e, ok := f.decode(event.Event)
	if !ok || e.Mentioned || !e.Group {
		t.Fatal("another user's mention triggered the bot")
	}
	event.Event.Message.Mentions[0].Id.OpenId = &f.botID
	e, ok = f.decode(event.Event)
	if !ok || !e.Mentioned || e.Text != "hello" || e.UserID != "ou_alice" || e.ThreadID != "omt_thread" {
		t.Fatalf("incorrect Feishu message: %#v", e)
	}
	var replies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"feishu-token","expire":7200}`))
			return
		}
		if r.URL.Path != "/open-apis/im/v1/messages/om_source/reply" || r.Header.Get("Authorization") != "Bearer feishu-token" {
			t.Error("Feishu reply escaped the source message")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		replies = append(replies, body)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()
	f.base = server.URL
	for index := 0; index < 2; index++ {
		if err := f.Send(context.Background(), e, "**已完成**\n```go\nhello()\n```", "reply-id"); err != nil {
			t.Fatal(err)
		}
	}
	if len(replies) != 2 || replies[0]["uuid"] != replies[1]["uuid"] || replies[0]["reply_in_thread"] != true || replies[0]["msg_type"] != "post" || !strings.Contains(replies[0]["content"].(string), `"tag":"md"`) {
		t.Fatal("Feishu lost markdown, thread routing or retry identity")
	}
	if got := feishuPostText(`{"zh_cn":{"title":"标题","content":[[{"tag":"text","text":"正文"}],[{"tag":"a","text":"链接","href":"https://example.com"}]]}}`); got != "标题\n正文\n链接 (https://example.com)" {
		t.Fatalf("rich-text intake lost text: %q", got)
	}
}

func TestUTF8SplittingIsLosslessAndPlatformErrorsDoNotLeakBodies(t *testing.T) {
	text := strings.Repeat("中文🙂\n", 2000)
	chunks := SplitText(text, 3500)
	if strings.Join(chunks, "") != text {
		t.Fatal("chunking lost content")
	}
	for _, chunk := range chunks {
		if len(chunk) > 3500 || !utf8.ValidString(chunk) {
			t.Fatal("chunk exceeds platform budget or corrupts UTF-8")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"private-token"}`))
	}))
	defer server.Close()
	err := platformJSON(context.Background(), newHTTPClient(), "POST", server.URL, nil, nil, nil)
	if err == nil || strings.Contains(err.Error(), "private-token") || !strings.Contains(err.Error(), "401") {
		t.Fatal("platform error leaked its raw response")
	}
}
