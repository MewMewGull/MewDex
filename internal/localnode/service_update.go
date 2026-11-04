package localnode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/store"
)

// CheckApplicationUpdate is the installer's read-only preflight. It also
// inspects stopped installations so pending recovery cannot be overwritten.
func (m *Manager) CheckApplicationUpdate(ctx context.Context) error {
	lock, err := lifecycle.Acquire(filepath.Join(m.options.Root, "management"))
	if err != nil {
		return errors.New("another local service operation is in progress")
	}
	defer lock.Close()
	state, err := m.status(ctx)
	if err != nil {
		return err
	}
	if state.State == "unconfigured" {
		return nil
	}
	if state.State != "running" && state.State != "stopped" {
		return errors.New("请先在工作台中核对本机服务状态，再更新应用")
	}
	c, err := config.Load(m.profile())
	if err != nil {
		return err
	}
	return updateIdle(c)
}

// Apply the helper supplied by this desktop installation. Existing credentials,
// engine selection, workspaces and data stay in their original locations.
func (m *Manager) updateService(ctx context.Context, expectedInstance string) error {
	c, err := config.Load(m.profile())
	if err != nil {
		return err
	}
	state, err := m.status(ctx)
	if err != nil {
		return err
	}
	if state.State != "running" && state.State != "stopped" {
		return errors.New("请先在本机设备中确认服务状态，再更新")
	}
	if state.Ready && (expectedInstance == "" || state.InstanceID != expectedInstance) {
		return errors.New("本机服务已变化，请重试创建项目")
	}
	if err := updateIdle(c); err != nil {
		return err
	}
	startup, err := m.startupStatus(ctx)
	if err != nil {
		return err
	}
	if _, err = runtimefiles.Program(m.options.Root, m.options.Program); err != nil {
		return err
	}
	if state.Ready {
		if err = m.stop(ctx, expectedInstance); err != nil {
			return err
		}
	}
	if startup.RequestedEnabled {
		if _, err = m.applyStartup(ctx, &StartupRequest{Enabled: true}); err != nil {
			return fmt.Errorf("本机服务已停止，更新自动启动入口失败：%w", err)
		}
	}
	if err = m.start(ctx); err != nil {
		return fmt.Errorf("组件已准备，重新连接本机服务失败：%w", err)
	}
	return nil
}

func updateIdle(c config.Config) error {
	for _, target := range []struct {
		name     string
		enabled  bool
		executor bool
	}{{"executor.db", c.Executor, true}, {"hub.db", c.Hub, false}} {
		if !target.enabled {
			continue
		}
		path := filepath.Join(c.DataDir, target.name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		database, err := store.Open(path)
		if err != nil {
			return err
		}
		sessions, err := store.List[protocol.Session](database, "sessions")
		database.Close()
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if session.ExecutionRecovery != nil && session.ExecutionRecovery.State == "retired" {
				continue
			}
			busy := session.State == "running" || session.State == "stopping" || session.StopStatus == "background-running"
			if target.executor {
				busy = session.ExecutionUnconfirmed()
			}
			if busy {
				return fmt.Errorf("“%s”仍在执行或等待确认，请处理完成后再创建项目；已有填写会保留", session.Title)
			}
		}
	}
	return nil
}
