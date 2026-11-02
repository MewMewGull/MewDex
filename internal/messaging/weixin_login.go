package messaging

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"mewdex/internal/protocol"
)

type weixinLogin struct {
	Owner   string
	QRCode  string
	URL     string
	Base    string
	Expires time.Time
}

func (m *Manager) BeginWeixinLogin(ctx context.Context, owner string) (protocol.MessagingLogin, error) {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	if m.ctx != nil && m.ctx.Err() != nil {
		return protocol.MessagingLogin{}, errors.New("消息服务正在停止")
	}
	var response struct {
		QRCode string `json:"qrcode"`
		URL    string `json:"qrcode_img_content"`
	}
	if err := platformJSON(ctx, m.loginClient, "GET", weixinBase+"/ilink/bot/get_bot_qrcode?bot_type=3", weixinHeaders(""), nil, &response); err != nil {
		return protocol.MessagingLogin{}, err
	}
	if response.QRCode == "" || response.URL == "" || len(response.URL) > 8192 {
		return protocol.MessagingLogin{}, errors.New("微信未返回有效的登录二维码")
	}
	m.login = &weixinLogin{Owner: owner, QRCode: response.QRCode, URL: response.URL, Base: weixinBase, Expires: time.Now().Add(8 * time.Minute)}
	return protocol.MessagingLogin{State: "wait", URL: response.URL}, nil
}

// QR responses can name a regional host. Only Tencent's Weixin domain may
// receive the bot credential; never follow an arbitrary response URL.
func trustedWeixinBase(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" || parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("微信返回了无效的服务地址")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "ilinkai.weixin.qq.com" && !strings.HasSuffix(host, ".weixin.qq.com") {
		return "", errors.New("微信服务地址不属于可信域名")
	}
	return "https://" + host, nil
}

func (m *Manager) PollWeixinLogin(ctx context.Context, owner string) (protocol.MessagingLogin, error) {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	if m.ctx != nil && m.ctx.Err() != nil {
		return protocol.MessagingLogin{}, errors.New("消息服务正在停止")
	}
	login := m.login
	if login == nil || login.Owner != owner {
		return protocol.MessagingLogin{}, errors.New("请先生成自己的微信登录二维码")
	}
	if time.Now().After(login.Expires) {
		m.login = nil
		return protocol.MessagingLogin{State: "expired", URL: login.URL}, nil
	}
	var response struct {
		Status       string `json:"status"`
		RedirectHost string `json:"redirect_host"`
		BaseURL      string `json:"baseurl"`
		BotToken     string `json:"bot_token"`
		BotID        string `json:"ilink_bot_id"`
		UserID       string `json:"ilink_user_id"`
	}
	if err := platformJSON(ctx, m.loginClient, "GET", login.Base+"/ilink/bot/get_qrcode_status?qrcode="+url.QueryEscape(login.QRCode), weixinHeaders(""), nil, &response); err != nil {
		return protocol.MessagingLogin{}, err
	}
	if response.Status == "scaned_but_redirect" {
		base, err := trustedWeixinBase("https://" + response.RedirectHost)
		if err != nil {
			return protocol.MessagingLogin{}, err
		}
		login.Base = base
	}
	if response.Status == "confirmed" {
		if response.BotID == "" || response.BotToken == "" {
			return protocol.MessagingLogin{}, errors.New("微信未返回完整的登录凭据")
		}
		base := login.Base
		var err error
		if response.BaseURL != "" {
			if base, err = trustedWeixinBase(response.BaseURL); err != nil {
				return protocol.MessagingLogin{}, err
			}
		}
		c, _ := m.config("weixin")
		c.AccountID, c.Secret, c.BaseURL = response.BotID, response.BotToken, base
		// Scanning authenticates the scanning user. It does not enable a disabled
		// channel or grant any other account or group access.
		if response.UserID != "" && len(c.AllowedUsers) == 0 {
			c.AllowedUsers = []string{response.UserID}
		}
		if err = m.save(c); err != nil {
			return protocol.MessagingLogin{}, err
		}
		m.login = nil
	}
	if response.Status == "expired" {
		m.login = nil
	}
	return protocol.MessagingLogin{State: response.Status, URL: login.URL}, nil
}
