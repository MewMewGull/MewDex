package localnode

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"mewdex/internal/backhaul"
	"mewdex/internal/config"
	"mewdex/internal/hubproxy"
	"mewdex/internal/lifecycle"
	"mewdex/internal/startup"
)

type HubProxySettings hubproxy.Settings

type HubProxyInfo struct {
	Settings        HubProxySettings `json:"settings"`
	State           string           `json:"state"`
	Message         string           `json:"message,omitempty"`
	LegacyAvailable bool             `json:"legacyAvailable"`
}

func (m *Manager) proxyStatus(ctx context.Context) (*HubProxyInfo, error) {
	c, err := config.Read(m.profile())
	if err != nil {
		return nil, err
	}
	if !c.Hub {
		return nil, errors.New("反向代理只能由 Hub 管理")
	}
	s, fingerprint, err := hubproxy.Read(m.options.Root)
	if err != nil {
		return nil, err
	}
	info := &HubProxyInfo{Settings: HubProxySettings(s), State: "disabled"}
	_, err = os.Stat(filepath.Join(m.options.Root, "ingress", "backhaul.json"))
	_, migrated := os.Stat(filepath.Join(m.options.Root, "ingress", "hub-managed"))
	info.LegacyAvailable = err == nil && errors.Is(migrated, os.ErrNotExist)
	if !s.Enabled {
		return info, nil
	}
	info.State, info.Message = "waiting", "设置已保存，Hub 启动后自动连接。"
	record, err := lifecycle.Read(c.DataDir)
	if err != nil {
		return info, nil
	}
	if _, err = lifecycle.Call(ctx, record, false); err != nil {
		return info, nil
	}
	state, err := hubproxy.ReadState(c.DataDir)
	updated, timeErr := time.Parse(time.RFC3339Nano, state.UpdatedAt)
	if err == nil && timeErr == nil && state.InstanceID == record.InstanceID && state.Fingerprint == fingerprint && time.Since(updated) >= -30*time.Second && time.Since(updated) < 40*time.Second {
		info.State, info.Message = state.State, state.Message
	} else {
		info.Message = "等待 Hub 采用设置；旧版 Hub 需要在“数据与更新”中更新服务。"
	}
	return info, nil
}

func (m *Manager) applyProxy(ctx context.Context, request *HubProxySettings) (*HubProxyInfo, error) {
	if request == nil {
		return nil, errors.New("请提供代理设置")
	}
	c, err := config.Read(m.profile())
	if err != nil {
		return nil, err
	}
	if !c.Hub {
		return nil, errors.New("反向代理只能由 Hub 管理")
	}
	if request.Enabled {
		if _, err = hubproxy.LocalTarget(c.Listen); err != nil {
			return nil, err
		}
	}
	if err = hubproxy.Save(m.options.Root, hubproxy.Settings(*request)); err != nil {
		return nil, err
	}
	return m.proxyStatus(ctx)
}

// Import the exact old component owned by this installation. Its login entry
// is removed and its own control channel is stopped before enabling the Hub
// listener. Never select/kill processes by executable name or remote port.
func (m *Manager) importProxy(ctx context.Context) (*HubProxyInfo, error) {
	c, err := config.Read(m.profile())
	if err != nil {
		return nil, err
	}
	if !c.Hub {
		return nil, errors.New("反向代理只能由 Hub 管理")
	}
	legacy := filepath.Join(m.options.Root, "ingress")
	old, _, err := backhaul.Read(legacy)
	if err != nil {
		return nil, err
	}
	s := hubproxy.Settings{Enabled: true, Host: old.Host, Port: old.Port, User: old.User, IdentityFile: old.IdentityFile, KnownHostsFile: old.KnownHostsFile, RemoteBind: old.RemoteBind, RemotePort: old.RemotePort, PublicURL: old.PublicURL}
	if err = s.Validate(); err != nil {
		return nil, err
	}
	target, err := hubproxy.LocalTarget(c.Listen)
	if err != nil {
		return nil, err
	}
	// A legacy profile must refer to this Hub's actual local port.
	if target != net.JoinHostPort("127.0.0.1", strconv.Itoa(old.LocalPort)) {
		return nil, errors.New("旧代理目标不是当前 Hub，请手动填写代理设置")
	}
	backend := m.options.StartupBackend
	if backend == nil {
		backend, err = startup.Native()
		if err != nil {
			return nil, err
		}
	}
	controller, err := startup.New(legacy, backend)
	if err != nil {
		return nil, err
	}
	if _, err = controller.Apply(ctx, false, "", startup.Binding{}); err != nil {
		return nil, err
	}
	data := filepath.Join(legacy, "data")
	if record, readErr := lifecycle.Read(data); readErr == nil {
		if record.DeviceID != old.ID {
			return nil, errors.New("旧代理实例身份已变化，未停止该实例")
		}
		if _, err = lifecycle.Call(ctx, record, true); err != nil {
			// A stale record is harmless only if both of its leases can be held.
			if err := legacyStopped(legacy, data); err != nil {
				return nil, errors.New("旧代理状态待确认；已关闭旧登录项，请核对后再次接管")
			}
		}
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if legacyStopped(legacy, data) == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("旧代理正在停止，请刷新后再次接管")
		case <-ticker.C:
		}
	}
	info, err := m.applyProxy(ctx, (*HubProxySettings)(&s))
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(legacy, "hub-managed"), []byte("Managed by MewDex Hub\n"), 0600); err != nil {
		return nil, err
	}
	info.LegacyAvailable = false
	return info, nil
}

func legacyStopped(root, data string) error {
	lease, err := lifecycle.Installation(root)
	if err != nil {
		return err
	}
	defer lease.Close()
	lock, err := lifecycle.Acquire(data)
	if err != nil {
		return err
	}
	return lock.Close()
}
