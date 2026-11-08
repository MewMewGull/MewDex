// Package computeruse owns explicitly granted, expiring UI sessions on an
// execution node. It is independent of model accounts and the command sandbox.
package computeruse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type Bounds struct {
	Left, Top, Width, Height int
}

type Reservation interface {
	io.Closer
	Interrupted() <-chan struct{}
}

type Driver interface {
	Available() (bool, string)
	Windows(context.Context) ([]protocol.ComputerWindow, error)
	Reserve(context.Context) (Reservation, error)
	Observe(context.Context, protocol.ComputerWindow) (protocol.ComputerObservation, Bounds, error)
	Act(context.Context, protocol.ComputerWindow, protocol.ComputerObservation, Bounds, protocol.ComputerAction) error
}

type activeSession struct {
	id          string
	ctx         context.Context
	cancel      context.CancelFunc
	reservation Reservation
	done        chan struct{}
	busy        bool
	observation protocol.ComputerObservation
	bounds      Bounds
}

type Manager struct {
	ctx      context.Context
	store    *store.Store
	deviceID string
	instance string
	driver   Driver
	mu       sync.Mutex
	active   *activeSession
	revoked  string
}

func New(ctx context.Context, database *store.Store, deviceID string, driver Driver) (*Manager, error) {
	m := &Manager{ctx: ctx, store: database, deviceID: deviceID, instance: protocol.ID(), driver: driver}
	value, err := database.ComputerSession()
	if errors.Is(err, store.ErrNotFound) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if value.DeviceID != deviceID {
		return nil, errors.New("computer control data belongs to another execution node")
	}
	if value.State != "stopped" && value.State != "expired" {
		value.State, value.Error = "unknown", "执行端已重启，原窗口授权已失效，请核对结果并停止原会话"
		if value.LastActionState == "running" {
			value.LastActionState = "unknown"
		}
		_, err = database.SaveComputerSession(value, value.Revision)
	}
	return m, err
}

func (m *Manager) Status() (protocol.ComputerStatus, error) {
	supported, reason := m.driver.Available()
	result := protocol.ComputerStatus{Supported: supported, Reason: reason}
	value, err := Current(m.store)
	if errors.Is(err, store.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Session = &value
	return result, nil
}

func (m *Manager) Request(owner, projectID, id, reason string) (protocol.ComputerStatus, error) {
	if owner == "" || projectID == "" || id == "" || strings.TrimSpace(reason) == "" || len(reason) > 4096 {
		return protocol.ComputerStatus{}, errors.New("computer control requires an owner, project and bounded reason")
	}
	if supported, reason := m.driver.Available(); !supported {
		return protocol.ComputerStatus{}, errors.New(reason)
	}
	prior, err := Current(m.store)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return protocol.ComputerStatus{}, err
	}
	if prior.ID == id && prior.OwnerID == owner && prior.ProjectID == projectID {
		return m.Status()
	}
	now := protocol.Now()
	value := protocol.ComputerSession{ID: id, DeviceID: m.deviceID, ProjectID: projectID, OwnerID: owner, InstanceID: m.instance, Reason: reason, State: "pending", ExpiresAt: time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano), CreatedAt: now}
	if _, err = m.store.SaveComputerSession(value, prior.Revision); err != nil {
		return protocol.ComputerStatus{}, err
	}
	return m.Status()
}

func expired(timestamp string) bool {
	value, err := time.Parse(time.RFC3339Nano, timestamp)
	return err != nil || !time.Now().Before(value)
}

func Current(database *store.Store) (protocol.ComputerSession, error) {
	value, err := database.ComputerSession()
	if err == nil && (value.State == "pending" || value.State == "approved") && expired(value.ExpiresAt) {
		value.State = "expired"
		value, err = database.SaveComputerSession(value, value.Revision)
		if errors.Is(err, store.ErrConflict) {
			return database.ComputerSession()
		}
	}
	return value, err
}

// Approve is used only by the authenticated local control panel. Hub requests
// cannot choose or approve a window. The selected PID is checked again by the
// native driver on every operation, including immediately before sending input.
func Approve(database *store.Store, input protocol.ComputerApproval, windows []protocol.ComputerWindow) (protocol.ComputerSession, error) {
	value, err := database.ComputerSession()
	if err != nil {
		return value, err
	}
	if value.ID != input.SessionID || value.Revision != input.ExpectedRevision || value.State != "pending" || expired(value.ExpiresAt) || input.DurationSeconds < 15 || input.DurationSeconds > 900 {
		return value, errors.New("窗口请求已变化或到期，请刷新后重新确认")
	}
	for _, window := range windows {
		if window.ID == input.WindowID && window.ProcessID != 0 && window.Title != "" {
			value.Window, value.State = &window, "approved"
			value.ExpiresAt = time.Now().Add(time.Duration(input.DurationSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
			return database.SaveComputerSession(value, value.Revision)
		}
	}
	return value, errors.New("所选窗口已关闭或无法授权，请刷新窗口列表")
}

func RequestStop(database *store.Store, sessionID string) (protocol.ComputerSession, error) {
	value, err := database.ComputerSession()
	if err != nil || value.ID != sessionID {
		return value, errors.New("computer session changed; refresh its identity")
	}
	if value.State == "stopped" || value.State == "expired" {
		return value, nil
	}
	previous := value.State
	value.State = "stopping"
	if previous == "pending" || previous == "approved" || previous == "unknown" {
		value.State = "stopped"
	}
	if value.LastActionState == "running" {
		value.LastActionState = "unknown"
	}
	return database.SaveComputerSession(value, value.Revision)
}

func (m *Manager) Stop(ctx context.Context, sessionID, owner string) (protocol.ComputerStatus, error) {
	value, err := m.owned(sessionID, owner)
	if err != nil {
		return protocol.ComputerStatus{}, err
	}
	if _, err = RequestStop(m.store, value.ID); err != nil {
		return protocol.ComputerStatus{}, err
	}
	m.mu.Lock()
	active := m.active
	if active != nil && active.id == sessionID {
		active.cancel()
	}
	m.mu.Unlock()
	if active != nil && active.id == sessionID {
		select {
		case <-active.done:
		case <-ctx.Done():
			return protocol.ComputerStatus{}, errors.New("停止结果尚未确认，请查询原会话")
		}
	} else {
		value, err = m.store.ComputerSession()
		if err == nil && value.ID == sessionID && value.State == "stopping" {
			value.State = "stopped"
			_, err = m.store.SaveComputerSession(value, value.Revision)
		}
		if err != nil {
			return protocol.ComputerStatus{}, err
		}
	}
	return m.Status()
}

func (m *Manager) owned(sessionID, owner string) (protocol.ComputerSession, error) {
	value, err := m.store.ComputerSession()
	if err != nil || value.ID != sessionID || value.OwnerID != owner || value.DeviceID != m.deviceID {
		return value, errors.New("computer session is outside this turn's ownership")
	}
	return value, nil
}

func (m *Manager) begin(value protocol.ComputerSession) (*activeSession, error) {
	if value.InstanceID != m.instance || value.Window == nil || expired(value.ExpiresAt) || value.State != "approved" && value.State != "ready" {
		return nil, errors.New("请先在执行端本机面板授权当前窗口，会话需要保持有效")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.revoked == value.ID {
		return nil, errors.New("窗口控制已停止，请申请新的本机授权")
	}
	if m.active != nil {
		if m.active.id == value.ID && m.active.ctx.Err() == nil {
			return m.active, nil
		}
		return nil, errors.New("桌面正由另一会话占用，请先停止原会话")
	}
	deadline, _ := time.Parse(time.RFC3339Nano, value.ExpiresAt)
	ctx, cancel := context.WithDeadline(m.ctx, deadline)
	reservation, err := m.driver.Reserve(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	value.State = "ready"
	if _, err = m.store.SaveComputerSession(value, value.Revision); err != nil {
		cancel()
		_ = reservation.Close()
		return nil, err
	}
	active := &activeSession{id: value.ID, ctx: ctx, cancel: cancel, reservation: reservation, done: make(chan struct{})}
	m.active = active
	go m.watch(active)
	return active, nil
}

func (m *Manager) watch(active *activeSession) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	reason := "Computer Use 已停止"
	for active.ctx.Err() == nil {
		select {
		case <-active.ctx.Done():
		case <-active.reservation.Interrupted():
			reason = "用户按下 Escape，窗口控制已停止"
			active.cancel()
		case <-ticker.C:
			value, err := m.store.ComputerSession()
			if err != nil || value.ID != active.id || value.State == "stopping" || expired(value.ExpiresAt) {
				if err == nil && expired(value.ExpiresAt) {
					reason = "窗口授权已到期"
				}
				active.cancel()
			}
		}
	}
	m.mu.Lock()
	m.revoked = active.id
	m.mu.Unlock()
	if errors.Is(active.ctx.Err(), context.DeadlineExceeded) {
		reason = "窗口授权已到期"
	}
	// A stopped session cannot free its desktop reservation until its owned
	// native worker has actually returned or been terminated.
	for {
		m.mu.Lock()
		busy := active.busy
		m.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	closeErr := active.reservation.Close()
	value, err := m.store.ComputerSession()
	if err == nil && value.ID == active.id {
		value.State, value.Error = "stopped", reason
		if closeErr != nil {
			value.State, value.Error = "unknown", closeErr.Error()
		}
		if value.LastActionState == "running" {
			value.LastActionState = "unknown"
		}
		_, _ = m.store.SaveComputerSession(value, value.Revision)
	}
	m.mu.Lock()
	if m.active == active {
		m.active = nil
	}
	m.mu.Unlock()
	close(active.done)
}

func (m *Manager) Operate(ctx context.Context, request protocol.ComputerRequest, actionID string) (protocol.ComputerResult, error) {
	value, err := m.owned(request.SessionID, request.OwnerID)
	if err != nil {
		return protocol.ComputerResult{}, err
	}
	active, err := m.begin(value)
	if err != nil {
		return protocol.ComputerResult{}, err
	}
	m.mu.Lock()
	if active.busy {
		m.mu.Unlock()
		return protocol.ComputerResult{}, errors.New("窗口正在执行另一项操作，请查询原指令")
	}
	if request.Action != nil && (request.ObservationID == "" || request.ObservationID != active.observation.ID || expiredObservation(active.observation.CreatedAt)) {
		m.mu.Unlock()
		return protocol.ComputerResult{}, errors.New("请先读取当前窗口，操作需要引用最近一次观察编号")
	}
	active.busy = true
	observation, bounds := active.observation, active.bounds
	active.observation = protocol.ComputerObservation{}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); active.busy = false; m.mu.Unlock() }()
	operationCtx, cancel := context.WithTimeout(active.ctx, 8*time.Second)
	defer cancel()
	stopCaller := context.AfterFunc(ctx, cancel)
	defer stopCaller()
	value, err = m.owned(request.SessionID, request.OwnerID)
	if err != nil || value.State != "ready" || expired(value.ExpiresAt) || operationCtx.Err() != nil {
		return protocol.ComputerResult{}, errors.New("窗口授权或请求已结束")
	}
	if request.Action != nil {
		if err = validateAction(*request.Action, observation); err != nil {
			return protocol.ComputerResult{}, err
		}
		value.LastActionID, value.LastActionState = actionID, "running"
		value, err = m.store.SaveComputerSession(value, value.Revision)
		if err != nil {
			return protocol.ComputerResult{}, err
		}
		if err = m.driver.Act(operationCtx, *value.Window, observation, bounds, *request.Action); err != nil {
			value.LastActionState, value.Error = "unknown", "界面操作结果尚未确认，请读取当前窗口后核对"
			_, _ = m.store.SaveComputerSession(value, value.Revision)
			active.cancel()
			return protocol.ComputerResult{}, fmt.Errorf("界面操作结果尚未确认 %w", err)
		}
		value.LastActionState = "completed"
	}
	observation, bounds, err = m.driver.Observe(operationCtx, *value.Window)
	if err != nil {
		value.Error = "窗口观察失败，请重新读取后再操作"
		_, _ = m.store.SaveComputerSession(value, value.Revision)
		return protocol.ComputerResult{}, err
	}
	observation.ID, observation.SessionID, observation.CreatedAt = protocol.ID(), value.ID, protocol.Now()
	value.LastObservationID, value.Error = observation.ID, ""
	if _, err = m.store.SaveComputerSession(value, value.Revision); err != nil {
		return protocol.ComputerResult{}, errors.New("窗口结果已返回但状态保存失败，请核对原指令")
	}
	m.mu.Lock()
	active.observation, active.bounds = observation, bounds
	m.mu.Unlock()
	status, err := m.Status()
	return protocol.ComputerResult{Status: status, Observation: &observation}, err
}

func expiredObservation(timestamp string) bool {
	value, err := time.Parse(time.RFC3339Nano, timestamp)
	return err != nil || time.Since(value) > 30*time.Second
}

func validateAction(action protocol.ComputerAction, observation protocol.ComputerObservation) error {
	switch action.Kind {
	case "click", "scroll":
		if action.X == nil || action.Y == nil || *action.X < 0 || *action.Y < 0 || *action.X >= observation.Width || *action.Y >= observation.Height || action.Button != "" && action.Button != "left" && action.Button != "right" || action.Scroll < -10 || action.Scroll > 10 || action.Kind == "scroll" && action.Scroll == 0 {
			return errors.New("界面坐标、按钮或滚动幅度超出范围")
		}
	case "type":
		if action.Text == "" || len(action.Text) > 4096 || strings.IndexFunc(action.Text, func(r rune) bool { return r < 32 && r != '\n' && r != '\t' }) >= 0 {
			return errors.New("输入文本为空、过长或包含控制字符")
		}
	case "key":
		if len(action.Keys) == 0 || len(action.Keys) > 3 {
			return errors.New("按键组合需要包含 1 至 3 个受支持按键")
		}
	default:
		return errors.New("unsupported computer action")
	}
	return nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	active := m.active
	if active != nil {
		active.cancel()
	}
	m.mu.Unlock()
	if active != nil {
		<-active.done
	}
}

func (m *Manager) Interrupt(reason string) {
	before, beforeErr := m.store.ComputerSession()
	m.Close()
	if beforeErr != nil || before.State == "stopped" || before.State == "expired" {
		return
	}
	value, err := m.store.ComputerSession()
	if err == nil && value.ID == before.ID {
		if value.State != "unknown" {
			value.State = "stopped"
		}
		value.Error = reason + "\n" + value.Error
		if value.LastActionState == "running" {
			value.LastActionState = "unknown"
		}
		_, _ = m.store.SaveComputerSession(value, value.Revision)
	}
}
