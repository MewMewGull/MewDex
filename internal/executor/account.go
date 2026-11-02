package executor

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func (x *Executor) accountCommand(ctx context.Context, c protocol.Command) protocol.CommandResult {
	var result any
	var err error
	switch c.Kind {
	case "account.read":
		var response struct {
			Account *struct {
				Type     string `json:"type"`
				PlanType string `json:"planType"`
				Email    string `json:"email"`
			} `json:"account"`
		}
		err = x.engine.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &response)
		account := protocol.ModelAccount{SignedIn: response.Account != nil, LoginSupported: x.config.Engine.Home != "", LogoutSupported: true}
		if response.Account != nil {
			account.Type = response.Account.Type
			account.PlanType = response.Account.PlanType
			account.Email = response.Account.Email
		}
		result = account
	case "account.limits":
		var response protocol.ModelRateLimits
		err = x.engine.Call(ctx, "account/rateLimits/read", map[string]any{}, &response)
		result = response
	case "account.logout":
		var response json.RawMessage
		err = x.engine.Call(ctx, "account/logout", map[string]any{}, &response)
		result = response
	case "account.login":
		if x.config.Engine.Home == "" {
			return protocol.CommandResult{State: "failed", Error: "this executor uses its existing local account configuration; MewDex login requires a dedicated engine.home"}
		}
		var response protocol.DeviceLogin
		err = x.engine.Call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &response)
		if err == nil {
			parsed, e := url.Parse(response.VerificationURL)
			if e != nil || parsed.Scheme != "https" || (parsed.Hostname() != "chatgpt.com" && parsed.Hostname() != "openai.com" && !strings.HasSuffix(parsed.Hostname(), ".openai.com")) {
				err = errors.New("engine returned an unexpected official login URL")
			}
		}
		result = response
	case "account.cancel":
		if x.config.Engine.Home == "" {
			return protocol.CommandResult{State: "failed", Error: "this executor's login is managed outside MewDex"}
		}
		if c.RequestID == "" {
			err = errors.New("loginId required")
		} else {
			var response json.RawMessage
			err = x.engine.Call(ctx, "account/login/cancel", map[string]string{"loginId": c.RequestID}, &response)
			result = response
		}
	default:
		err = errors.New("unsupported account command")
	}
	if err != nil {
		return commandFailure(err)
	}
	return protocol.CommandResult{State: "completed", Data: protocol.JSON(result)}
}

func (x *Executor) accountNotification(n engine.Notification) {
	// Only public status fields are retained. Email and model credentials never enter Hub events.
	var values struct {
		AuthMode *string `json:"authMode"`
		PlanType *string `json:"planType"`
		LoginID  string  `json:"loginId,omitempty"`
		Success  bool    `json:"success"`
		Error    *string `json:"error,omitempty"`
	}
	if json.Unmarshal(n.Params, &values) == nil {
		_ = x.emit("", "engine."+n.Method, values)
	}
}
