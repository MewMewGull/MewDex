package localnode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
)

type ListenerRequest struct {
	Port int `json:"port"`
}

func (m *Manager) applyListener(ctx context.Context, request *ListenerRequest) error {
	if request == nil || request.Port < 1 || request.Port > 65535 {
		return errors.New("监听端口必须是 1–65535 之间的整数")
	}
	c, err := config.Read(m.profile())
	if err != nil {
		return err
	}
	if !c.Hub {
		return errors.New("只有 Hub 需要设置本机监听端口")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return err
	}
	listen := net.JoinHostPort(host, strconv.Itoa(request.Port))
	if listen == c.Listen {
		return nil
	}
	// Handle holds the installation lease; the data lease independently proves
	// no service is using this profile. Never stop a running service implicitly.
	data, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		return errors.New("请先停止并确认本机服务状态，再修改监听端口")
	}
	defer data.Close()
	startup, err := m.startupStatus(ctx)
	if err != nil {
		return err
	}
	c.Listen = listen
	if err := replaceJSON(m.profile(), c); err != nil {
		return err
	}
	if startup.RequestedEnabled {
		if _, err := m.applyStartup(ctx, &StartupRequest{Enabled: true}); err != nil {
			return fmt.Errorf("监听端口已保存，但自动启动入口更新失败：%w", err)
		}
	}
	return nil
}
