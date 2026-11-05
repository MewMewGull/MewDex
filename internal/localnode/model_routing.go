package localnode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
)

// Only the Hub has a model connection. Execution nodes use their paired Hub's
// control connection for all work and never select a model egress.
type ModelRoutingSettings struct {
	Route    string `json:"route"`
	ProxyURL string `json:"proxyUrl,omitempty"`
}

func modelRouting(c config.Config) ModelRoutingSettings {
	if !c.Hub {
		return ModelRoutingSettings{Route: "hub"}
	}
	settings := ModelRoutingSettings{Route: "local"}
	engine := c.HubModelEngine()
	if engine.ProxyTokenFile != "" || engine.ProxyCredential != "" {
		settings.Route = "custom"
	} else if engine.ProxyURL != "" {
		settings.Route, settings.ProxyURL = "proxy", engine.ProxyURL
	}
	return settings
}

func validateModelProxy(raw string, private bool) error {
	u, err := url.Parse(raw)
	if err != nil || config.ValidateURL(raw, private) != nil || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") {
		return errors.New("请填写有效的 HTTP / HTTPS 代理地址，且不能包含账号、密码、路径或查询参数")
	}
	return nil
}

func (m *Manager) applyModelRouting(ctx context.Context, request *ModelRoutingSettings) error {
	if request == nil {
		return errors.New("请填写 Hub 的 OpenAI 连接设置")
	}
	before, err := os.ReadFile(m.profile())
	if err != nil {
		return err
	}
	c, err := config.Read(m.profile())
	if err != nil {
		return err
	}
	if !c.Hub {
		return errors.New("模型连接由 Hub 管理")
	}
	data, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		return errors.New("请先停止 Hub，再修改 OpenAI 连接")
	}
	defer data.Close()
	switch request.Route {
	case "custom":
		if modelRouting(c).Route != "custom" || request.ProxyURL != "" {
			return errors.New("认证代理由 Hub 配置文件管理")
		}
	case "local", "proxy":
		proxy := strings.TrimSpace(request.ProxyURL)
		if request.Route == "proxy" {
			if err = validateModelProxy(proxy, true); err != nil {
				return err
			}
		} else {
			proxy = ""
		}
		c.Engine.ProxyURL, c.Engine.ProxyTokenFile, c.Engine.ProxyUsername = proxy, "", ""
	default:
		return errors.New("请选择 Hub 本机网络或 HTTP 代理")
	}
	c.Engine.UseHubGateway, c.Gateway = false, nil
	startup, err := m.startupStatus(ctx)
	if err != nil {
		return err
	}
	now, err := os.ReadFile(m.profile())
	if err != nil || !bytes.Equal(before, now) {
		return errors.New("Hub 配置已变化，请刷新后重试")
	}
	if err = replaceJSON(m.profile(), c); err != nil {
		return err
	}
	if startup.RequestedEnabled {
		if _, err = m.applyStartup(ctx, &StartupRequest{Enabled: true}); err != nil {
			return fmt.Errorf("OpenAI 连接已保存，自动启动入口更新失败 %w", err)
		}
	}
	return nil
}
