package localnode

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"mewdex/internal/computeruse"
	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func (m *Manager) computer(ctx context.Context, request Request) (Result, error) {
	c, err := config.Read(m.profile())
	if err != nil {
		return Result{}, err
	}
	if !c.Executor || c.Hub || request.ExpectedDeviceID == "" || request.ExpectedDeviceID != c.DeviceID {
		return Result{}, errors.New("refresh the independent execution node identity first")
	}
	snapshot, err := m.status(ctx)
	if err != nil || !snapshot.Ready {
		return Result{}, errors.New("请先启动执行端，再管理窗口授权")
	}
	database, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		return Result{}, err
	}
	defer database.Close()
	driver := computeruse.NewNativeDriver(m.options.Program)
	supported, reason := driver.Available()
	result := protocol.ComputerLocalStatus{Status: protocol.ComputerStatus{Supported: supported, Reason: reason}, Windows: []protocol.ComputerWindow{}}
	if request.Operation == "computer.approve" {
		if request.Computer == nil || !supported {
			return Result{}, errors.New("当前设备不支持此窗口授权请求")
		}
		windowCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		windows, listErr := driver.Windows(windowCtx)
		cancel()
		if listErr != nil {
			return Result{}, listErr
		}
		if _, err = computeruse.Approve(database, *request.Computer, windows); err != nil {
			return Result{}, err
		}
	} else if request.Operation == "computer.stop" {
		if request.Computer == nil {
			return Result{}, errors.New("computer session identity required")
		}
		if _, err = computeruse.RequestStop(database, request.Computer.SessionID); err != nil {
			return Result{}, err
		}
	} else if request.Operation != "computer.status" && request.Operation != "computer.windows" {
		return Result{}, errors.New("unsupported local computer operation")
	}
	if request.Operation == "computer.windows" && supported {
		windowCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result.Windows, err = driver.Windows(windowCtx)
		cancel()
		if err != nil {
			return Result{}, err
		}
	}
	value, err := computeruse.Current(database)
	if err == nil {
		result.Status.Session = &value
	} else if !errors.Is(err, store.ErrNotFound) {
		return Result{}, err
	}
	return Result{Snapshot: snapshot, Computer: &result}, nil
}
