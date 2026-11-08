// Package messaging connects external chats to ordinary, scoped MewDex inputs.
// Its adapter/envelope/session separation follows the Hermes messaging gateway;
// see docs/messaging.md for the pinned sources and supported platform features.
package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"mewdex/internal/protocol"
)

var platforms = []string{"weixin", "qq", "feishu"}

type channel struct {
	Platform  string `json:"platform"`
	Enabled   bool   `json:"enabled"`
	DeviceID  string `json:"deviceId"`
	ProjectID string `json:"projectId"`
	// Preserve conversation keys when a legacy channel's fixed project is cleared.
	BindingProjectID string   `json:"bindingProjectId,omitempty"`
	AllowedUsers     []string `json:"allowedUsers"`
	AllowedChats     []string `json:"allowedChats"`
	AppID            string   `json:"appId"`
	BotUserID        string   `json:"botUserId"`
	AccountID        string   `json:"accountId"`
	Secret           string   `json:"secret"`
	BaseURL          string   `json:"baseUrl,omitempty"`
	IdentityID       string   `json:"identityId"`
}

// Envelope carries only normalized text and an opaque platform reply context.
// Platform IDs remain separate from MewDex IDs, paths and execution permissions.
type Envelope struct {
	ID         string `json:"id"`
	Platform   string `json:"platform"`
	Account    string `json:"account"`
	ChatID     string `json:"chatId"`
	UserID     string `json:"userId"`
	ThreadID   string `json:"threadId,omitempty"`
	Scene      string `json:"scene"`
	Text       string `json:"text"`
	ReplyToken string `json:"replyToken,omitempty"`
	Mentioned  bool   `json:"mentioned"`
	Group      bool   `json:"group"`
}

type binding struct {
	ID             string   `json:"id"`
	Envelope       Envelope `json:"envelope"`
	IdentityID     string   `json:"identityId"`
	DeviceID       string   `json:"deviceId"`
	ProjectID      string   `json:"projectId"`
	ProjectChoices []string `json:"projectChoices,omitempty"`
	SessionID      string   `json:"sessionId"`
	CreateID       string   `json:"createId"`
	Cursor         int64    `json:"cursor"`
}

type inbox struct {
	ID        string            `json:"id"`
	BindingID string            `json:"bindingId"`
	Envelope  Envelope          `json:"envelope"`
	State     string            `json:"state"`
	CreatedAt string            `json:"createdAt"`
	Command   *protocol.Command `json:"command,omitempty"`
}

type delivery struct {
	ID         string   `json:"id"`
	BindingID  string   `json:"bindingId"`
	Envelope   Envelope `json:"envelope"`
	IdentityID string   `json:"identityId,omitempty"`
	DeviceID   string   `json:"deviceId,omitempty"`
	ProjectID  string   `json:"projectId,omitempty"`
	Text       string   `json:"text"`
	State      string   `json:"state"`
	CreatedAt  string   `json:"createdAt"`
	Attempts   int      `json:"attempts"`
	NextAt     int64    `json:"nextAt"`
}

type adapter interface {
	Run(context.Context, func(Envelope) error, func(string, string)) error
	Send(context.Context, Envelope, string, string) error
}

type Submit func(context.Context, string, protocol.Command) (protocol.CommandResult, error)
type factory func(channel) adapter

func stable(parts ...string) string {
	sum := sha256.Sum256(protocol.JSON(parts))
	return hex.EncodeToString(sum[:])[:40]
}

func (c channel) account() string {
	if c.Platform == "weixin" {
		return c.AccountID
	}
	return c.AppID
}

func (c channel) accepts(e Envelope) bool {
	return c.Enabled && e.Account == c.account() && e.UserID != "" && e.ChatID != "" &&
		slices.Contains(c.AllowedUsers, e.UserID) &&
		(!e.Group || e.Mentioned && slices.Contains(c.AllowedChats, e.ChatID))
}

func (c channel) key(e Envelope) string {
	project := c.ProjectID
	if project == "" {
		project = c.BindingProjectID
	}
	return stable(c.Platform, c.account(), c.DeviceID, project, e.Scene, e.ChatID, e.ThreadID, e.UserID)
}

func normalizedIDs(values []string) ([]string, error) {
	result := []string{}
	if len(values) > 200 {
		return nil, errors.New("最多允许 200 个用户或群聊")
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
			return nil, errors.New("用户和群聊 ID 必须非空，且不含换行")
		}
		if !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result, nil
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("消息平台不允许自动跟随重定向")
	}}
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// SplitText preserves UTF-8 and keeps every REST message within its byte budget.
func SplitText(text string, limit int) []string {
	if limit < 1 {
		return nil
	}
	var result []string
	for len(text) > limit {
		end := limit
		for end > 0 && text[end]&0xc0 == 0x80 {
			end--
		}
		if end == 0 {
			return nil
		}
		if newline := strings.LastIndexByte(text[:end], '\n'); newline > end/2 {
			end = newline + 1
		}
		result = append(result, text[:end])
		text = text[end:]
	}
	if text != "" {
		result = append(result, text)
	}
	return result
}
