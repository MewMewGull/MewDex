package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type qq struct {
	config   channel
	store    *store.Store
	client   *http.Client
	base     string
	tokenURL string
	tokenMu  sync.Mutex
	token    string
	expires  time.Time
	stateMu  sync.Mutex
	session  qqSession
}

type qqSession struct {
	ID    string `json:"id"`
	Seq   int64  `json:"seq"`
	BotID string `json:"botId"`
}

func newQQ(c channel, s *store.Store) *qq {
	q := &qq{config: c, store: s, client: newHTTPClient(), base: "https://api.sgroup.qq.com", tokenURL: "https://bots.qq.com/app/getAppAccessToken"}
	_ = s.Get("messaging_qq_session", c.AppID, &q.session)
	return q
}

func (q *qq) accessToken(ctx context.Context) (string, error) {
	q.tokenMu.Lock()
	defer q.tokenMu.Unlock()
	if q.token != "" && time.Until(q.expires) > time.Minute {
		return q.token, nil
	}
	var response struct {
		Token   string          `json:"access_token"`
		Expires json.RawMessage `json:"expires_in"`
	}
	if err := platformJSON(ctx, q.client, "POST", q.tokenURL, nil, map[string]string{"appId": q.config.AppID, "clientSecret": q.config.Secret}, &response); err != nil {
		return "", err
	}
	var seconds int64
	if json.Unmarshal(response.Expires, &seconds) != nil {
		var value string
		_ = json.Unmarshal(response.Expires, &value)
		seconds, _ = strconv.ParseInt(value, 10, 64)
	}
	if response.Token == "" || seconds < 1 {
		return "", errors.New("QQ 未返回有效访问凭据")
	}
	q.token, q.expires = response.Token, time.Now().Add(time.Duration(min(seconds, 86400))*time.Second)
	return q.token, nil
}

func (q *qq) api(ctx context.Context, method, path string, body any, response any) error {
	token, err := q.accessToken(ctx)
	if err != nil {
		return err
	}
	headers := http.Header{"Authorization": {"QQBot " + token}, "X-Union-Appid": {q.config.AppID}}
	err = platformJSON(ctx, q.client, method, q.base+path, headers, body, response)
	var failure *platformError
	if errors.As(err, &failure) && failure.Status == http.StatusUnauthorized {
		q.tokenMu.Lock()
		q.token = ""
		q.tokenMu.Unlock()
	}
	return err
}

type qqFrame struct {
	Op   int             `json:"op"`
	Seq  *int64          `json:"s"`
	Type string          `json:"t"`
	Data json.RawMessage `json:"d"`
}

func (q *qq) Run(ctx context.Context, receive func(Envelope) error, status func(string, string)) error {
	var endpoint struct {
		URL string `json:"url"`
	}
	if err := q.api(ctx, "GET", "/gateway", nil, &endpoint); err != nil {
		return err
	}
	u, err := url.Parse(endpoint.URL)
	if err != nil || u.User != nil || u.Fragment != "" || u.Scheme != "wss" || !strings.HasSuffix(u.Hostname(), ".qq.com") {
		// An internal HTTP test transport can use a loopback websocket.
		if err != nil || !strings.HasPrefix(q.base, "http://127.0.0.1:") || u.Scheme != "ws" || u.Hostname() != "127.0.0.1" {
			return errors.New("QQ 返回了无效的网关地址")
		}
	}
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	connection, _, err := websocket.Dial(dialCtx, endpoint.URL, &websocket.DialOptions{HTTPClient: q.client})
	cancel()
	if err != nil {
		return errors.New("QQ 长连接失败")
	}
	defer connection.CloseNow()
	connection.SetReadLimit(1024 * 1024)
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	_, data, err := connection.Read(readCtx)
	cancel()
	var hello qqFrame
	var helloData struct {
		Interval int `json:"heartbeat_interval"`
	}
	if err != nil || json.Unmarshal(data, &hello) != nil || hello.Op != 10 || json.Unmarshal(hello.Data, &helloData) != nil || helloData.Interval < 1000 || helloData.Interval > 120000 {
		return errors.New("QQ 网关握手无效")
	}
	token, err := q.accessToken(ctx)
	if err != nil {
		return err
	}
	q.stateMu.Lock()
	session := q.session
	q.stateMu.Unlock()
	// C2C and group @messages share this intent. Requesting unrelated guild
	// intents can make QQ reject otherwise valid C2C/group-only applications.
	auth := map[string]any{"op": 2, "d": map[string]any{"token": "QQBot " + token, "intents": 1 << 25, "shard": []int{0, 1}, "properties": map[string]string{"$os": runtime.GOOS, "$browser": "MewDex", "$device": "MewDex"}}}
	if session.ID != "" {
		auth = map[string]any{"op": 6, "d": map[string]any{"token": "QQBot " + token, "session_id": session.ID, "seq": session.Seq}}
	}
	if err = connection.Write(ctx, websocket.MessageText, protocol.JSON(auth)); err != nil {
		return errors.New("QQ 网关鉴权失败")
	}
	runCtx, stop := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	defer func() { stop(); <-heartbeatDone }()
	var acknowledged atomic.Bool
	acknowledged.Store(true)
	heartbeat := func() error {
		q.stateMu.Lock()
		seq := q.session.Seq
		q.stateMu.Unlock()
		writeCtx, cancel := context.WithTimeout(runCtx, 8*time.Second)
		defer cancel()
		return connection.Write(writeCtx, websocket.MessageText, protocol.JSON(map[string]any{"op": 1, "d": seq}))
	}
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(time.Duration(helloData.Interval) * time.Millisecond * 4 / 5)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if !acknowledged.Swap(false) || heartbeat() != nil {
					_ = connection.CloseNow()
					return
				}
			}
		}
	}()
	for runCtx.Err() == nil {
		_, data, err := connection.Read(runCtx)
		if err != nil {
			return errors.New("QQ 长连接中断")
		}
		var frame qqFrame
		if json.Unmarshal(data, &frame) != nil {
			return errors.New("QQ 网关数据无效")
		}
		switch frame.Op {
		case 0:
			if frame.Type == "READY" {
				var ready struct {
					ID   string `json:"session_id"`
					User struct {
						ID string `json:"id"`
					} `json:"user"`
				}
				if json.Unmarshal(frame.Data, &ready) != nil || ready.ID == "" {
					return errors.New("QQ 未返回会话编号")
				}
				q.stateMu.Lock()
				q.session.ID, q.session.BotID = ready.ID, ready.User.ID
				q.stateMu.Unlock()
				status("connected", "QQ 官方机器人已连接")
			} else if frame.Type == "RESUMED" {
				status("connected", "QQ 会话已恢复")
			} else if e, ok := q.decode(frame); ok {
				if err = receive(e); err != nil {
					return err
				}
			}
			if frame.Seq != nil {
				q.stateMu.Lock()
				q.session.Seq = *frame.Seq
				checkpoint := q.session
				q.stateMu.Unlock()
				if err = q.store.Put("messaging_qq_session", q.config.AppID, checkpoint); err != nil {
					return err
				}
			}
		case 1:
			if err = heartbeat(); err != nil {
				return err
			}
		case 11:
			acknowledged.Store(true)
		case 9:
			var resumable bool
			_ = json.Unmarshal(frame.Data, &resumable)
			if !resumable {
				q.stateMu.Lock()
				q.session = qqSession{}
				q.stateMu.Unlock()
				_ = q.store.Delete("messaging_qq_session", q.config.AppID)
			}
			return errors.New("QQ 网关会话失效")
		case 7:
			return errors.New("QQ 请求重新连接")
		}
	}
	return runCtx.Err()
}

func (q *qq) decode(frame qqFrame) (Envelope, bool) {
	var message struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		GroupID string `json:"group_openid"`
		Author  struct {
			UserID   string `json:"user_openid"`
			MemberID string `json:"member_openid"`
			Bot      bool   `json:"bot"`
		} `json:"author"`
		Attachments []struct {
			ASR string `json:"asr_refer_text"`
		} `json:"attachments"`
	}
	if json.Unmarshal(frame.Data, &message) != nil || message.ID == "" || message.Author.Bot {
		return Envelope{}, false
	}
	e := Envelope{ID: message.ID, Platform: "qq", Account: q.config.AppID, Text: message.Content, ReplyToken: message.ID}
	switch frame.Type {
	case "C2C_MESSAGE_CREATE":
		e.UserID, e.ChatID, e.Scene = message.Author.UserID, message.Author.UserID, "c2c"
	case "GROUP_AT_MESSAGE_CREATE":
		e.UserID, e.ChatID, e.Scene, e.Group, e.Mentioned = message.Author.MemberID, message.GroupID, "group", true, true
	default:
		return Envelope{}, false
	}
	q.stateMu.Lock()
	botID := q.session.BotID
	q.stateMu.Unlock()
	if botID != "" {
		e.Text = strings.ReplaceAll(strings.ReplaceAll(e.Text, "<@!"+botID+">", ""), "<@"+botID+">", "")
	}
	for _, attachment := range message.Attachments {
		if attachment.ASR != "" {
			e.Text += "\n" + attachment.ASR
		}
	}
	e.Text = strings.TrimSpace(e.Text)
	return e, e.UserID != "" && e.ChatID != ""
}

func (q *qq) Send(ctx context.Context, e Envelope, text, id string) error {
	var path string
	switch e.Scene {
	case "c2c":
		path = "/v2/users/" + url.PathEscape(e.ChatID) + "/messages"
	case "group":
		path = "/v2/groups/" + url.PathEscape(e.ChatID) + "/messages"
	default:
		return errors.New("不支持的 QQ 回复目标")
	}
	for index, chunk := range SplitText(text, 3500) {
		seq, _ := strconv.ParseUint(stable(id, strconv.Itoa(index))[:7], 16, 32)
		body := map[string]any{"content": chunk, "msg_type": 0, "msg_id": e.ReplyToken, "msg_seq": seq + 1}
		var response struct {
			Code int `json:"code"`
		}
		if err := q.api(ctx, "POST", path, body, &response); err != nil {
			var failure *platformError
			if errors.As(err, &failure) && failure.Code == 40054005 {
				// QQ rejects an already accepted msg_id + msg_seq on retry.
				// Keep the stable identity and continue with remaining chunks.
				continue
			}
			return err
		}
		if response.Code != 0 {
			return fmt.Errorf("QQ 回复失败（%d）", response.Code)
		}
	}
	return nil
}
