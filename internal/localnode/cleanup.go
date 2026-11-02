package localnode

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type cleanupMarker struct {
	ID          string `json:"id,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	BootSession string `json:"bootSession,omitempty"`
}
type cleanupRecord struct {
	Format        int             `json:"format"`
	Info          CleanupInfo     `json:"info"`
	BeforeProfile string          `json:"beforeProfile"`
	RestartFrom   string          `json:"restartFrom"`
	Markers       []cleanupMarker `json:"markers"`
}
type cleanupIntent struct {
	Format           int    `json:"format"`
	PlanSHA256       string `json:"planSha256"`
	BootSession      string `json:"bootSession"`
	State            string `json:"state"`
	ExecutionCapture bool   `json:"executionCapture,omitempty"`
}
type cleanupView struct {
	info    CleanupInfo
	profile string
	dirs    []string
	markers []cleanupMarker
	boot    string
	leases  []*lifecycle.Lock
}

func (m *Manager) cleanupPlan(id string) string {
	return filepath.Join(m.options.Root, "cleanup-plans", id+".json")
}
func (m *Manager) cleanupIntentPath(id string) string {
	return filepath.Join(m.options.Root, "cleanup-plans", id+".apply.json")
}
func (m *Manager) cleanupBoot() (string, error) {
	read := m.options.BootSession
	if read == nil {
		read = lifecycle.BootSession
	}
	value, err := read()
	if err != nil {
		return "", err
	}
	return lifecycle.NormalizeBootSession(value)
}
func (view *cleanupView) close() {
	for i := len(view.leases) - 1; i >= 0; i-- {
		_ = view.leases[i].Close()
	}
}

// openCleanup never clears a marker. Its held OS leases exclude a living
// service, monitor and concurrent installation switch while a plan is reviewed.
func (m *Manager) openCleanup() (*cleanupView, error) {
	profileInfo, err := os.Lstat(m.profile())
	if err != nil {
		return nil, err
	}
	if !profileInfo.Mode().IsRegular() || profileInfo.Size() > 1<<20 {
		return nil, errors.New("cleanup recovery requires a bounded regular profile")
	}
	c, _, fingerprint, err := m.currentProfile()
	if err != nil || c == nil {
		return nil, errors.New("cleanup recovery requires the current configured device")
	}
	view := &cleanupView{info: CleanupInfo{DeviceID: c.DeviceID, Name: c.Name, State: "clear"}, profile: fingerprint, dirs: []string{filepath.Join(m.options.Root, "installation"), c.DataDir}}
	root, err := filepath.EvalSymlinks(m.options.Root)
	if err != nil {
		return nil, err
	}
	for _, dir := range view.dirs {
		actual, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, actual)
		if err != nil || relative == "." || !filepath.IsLocal(relative) {
			return nil, errors.New("cleanup recovery requires application-owned state directories")
		}
	}
	if view.dirs[0] == view.dirs[1] {
		return nil, errors.New("installation and data scopes must remain distinct")
	}
	for _, dir := range view.dirs {
		marker, err := lifecycle.ReadOwnership(dir)
		if err != nil {
			return nil, err
		}
		value := cleanupMarker{}
		if marker != nil {
			value = cleanupMarker{ID: marker.ID, Fingerprint: marker.Fingerprint, BootSession: marker.BootSession}
			view.info.Pending = true
		}
		view.markers = append(view.markers, value)
	}
	// A restored executor can have unknown project claims without a service
	// refusal marker. Inspect its existing state only under the same OS leases.
	if !view.info.Pending {
		if _, err := os.Lstat(filepath.Join(c.DataDir, "executor.db")); errors.Is(err, os.ErrNotExist) {
			return view, nil
		} else if err != nil {
			return nil, err
		}
	}
	for _, dir := range view.dirs {
		lease, err := lifecycle.Review(dir)
		if err != nil {
			view.close()
			view.leases = nil
			view.info.State = "in-use"
			view.info.Message = "本机服务或监护仍在运行，暂不能恢复。"
			return view, nil
		}
		view.leases = append(view.leases, lease)
	}
	// Refresh after the locks are held, rather than planning a racing read.
	view.info.Pending = false
	for i, dir := range view.dirs {
		marker, err := lifecycle.ReadOwnership(dir)
		if err != nil {
			view.close()
			return nil, err
		}
		value := cleanupMarker{}
		if marker != nil {
			value = cleanupMarker{ID: marker.ID, Fingerprint: marker.Fingerprint, BootSession: marker.BootSession}
			view.info.Pending = true
		}
		view.markers[i] = value
	}
	view.info.UnconfirmedExecutions, err = m.unconfirmedCleanupExecutions(view)
	if err != nil {
		view.close()
		return nil, err
	}
	if !cleanupWorkPending(view.info) {
		return view, nil
	}
	view.boot, err = m.cleanupBoot()
	if err != nil {
		view.info.State = "unavailable"
		view.info.Message = "暂时无法核对系统重新启动的记录，原状态已保留。"
		return view, nil
	}
	view.info.CanPrepare = true
	view.info.NeedsRestart = true
	view.info.State = "restart-required"
	return view, nil
}

func cleanupWorkPending(info CleanupInfo) bool {
	return info.Pending || info.UnconfirmedExecutions > 0
}

func (m *Manager) unconfirmedCleanupExecutions(view *cleanupView) (int, error) {
	path := filepath.Join(view.dirs[1], "executor.db")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, errors.New("execution recovery requires its existing regular database")
	}
	state, err := store.Open(path)
	if err != nil {
		return 0, err
	}
	defer state.Close()
	sessions, err := store.List[protocol.Session](state, "sessions")
	if err != nil {
		return 0, err
	}
	queued, err := store.List[protocol.QueueItem](state, "queue_items")
	if err != nil {
		return 0, err
	}
	uncertainQueue := map[string]bool{}
	for _, item := range queued {
		if item.DeviceID != view.info.DeviceID {
			return 0, errors.New("execution recovery queue belongs to another device")
		}
		if item.State == "dispatching" || item.State == "running" || item.State == "unknown" {
			uncertainQueue[item.SessionID] = true
		}
	}
	count := 0
	for _, session := range sessions {
		if session.DeviceID != view.info.DeviceID {
			return 0, errors.New("execution recovery data belongs to another device")
		}
		if !session.ExecutionUnconfirmed() && !uncertainQueue[session.ID] {
			continue
		}
		if !session.EngineStarted && !uncertainQueue[session.ID] {
			started, err := state.HasStarted(session.ID)
			if err != nil {
				return 0, err
			}
			if !started {
				continue
			}
		}
		recovery, err := state.ExecutionRecovery(session)
		if err != nil {
			return 0, err
		}
		if recovery == nil || recovery.State != "retired" {
			count++
		}
	}
	return count, nil
}

func (m *Manager) loadCleanup(id string) (cleanupRecord, error) {
	var record cleanupRecord
	if !localID(id) {
		return record, errors.New("a stable cleanup review ID is required")
	}
	if err := decodeLocalJSON(m.cleanupPlan(id), 64<<10, &record); err != nil {
		return record, err
	}
	if record.Format != 1 || record.Info.ID != id || len(record.Markers) != 2 || record.BeforeProfile == "" {
		return record, errors.New("invalid cleanup recovery plan")
	}
	boot, err := lifecycle.NormalizeBootSession(record.RestartFrom)
	if err != nil || boot != record.RestartFrom {
		return record, errors.New("invalid cleanup boot boundary")
	}
	return record, nil
}

func (m *Manager) readCleanupIntent(record cleanupRecord) (*cleanupIntent, error) {
	var intent cleanupIntent
	err := decodeLocalJSON(m.cleanupIntentPath(record.Info.ID), 4096, &intent)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	boot, bootErr := lifecycle.NormalizeBootSession(intent.BootSession)
	if intent.Format != 1 || intent.PlanSHA256 != hashBytes(protocol.JSON(record)) || bootErr != nil || boot != intent.BootSession || boot == record.RestartFrom || (intent.State != "committing" && intent.State != "applied") {
		return nil, errors.New("invalid cleanup recovery intent")
	}
	return &intent, nil
}

func markersMatch(expected, actual []cleanupMarker, partial bool) bool {
	if len(expected) != len(actual) {
		return false
	}
	for i, marker := range actual {
		if partial && marker.ID == "" {
			continue
		}
		if marker != expected[i] {
			return false
		}
	}
	return true
}

func (m *Manager) cleanupInfo(view *cleanupView, record cleanupRecord) (CleanupInfo, error) {
	info := view.info
	info.ID, info.CreatedAt = record.Info.ID, record.Info.CreatedAt
	intent, err := m.readCleanupIntent(record)
	if err != nil {
		return info, err
	}
	if view.profile != record.BeforeProfile {
		info.State, info.Message = "changed", "本机配置或待核实状态已变化，请重新准备恢复记录。"
		info.CanApply = false
		return info, nil
	}
	if info.State == "in-use" {
		// A running lifetime can temporarily replace a markerless checkpoint's
		// empty slots. Retain the review for display, but defer every adoption
		// and marker comparison until the OS leases can be acquired again.
		if intent != nil && intent.State == "applied" {
			info.State, info.Applied = "applied", true
		} else {
			info.Message = "请先停止本机服务，再重新检查此记录。原任务和检查记录仍保留。"
		}
		return info, nil
	}
	if !markersMatch(record.Markers, view.markers, intent != nil) {
		info.State, info.Message = "changed", "本机配置或待核实状态已变化，请重新准备恢复记录。"
		return info, nil
	}
	if !info.Pending && intent != nil && (!intent.ExecutionCapture || intent.State == "applied") {
		info.State, info.Applied, info.NeedsRestart = "applied", true, false
		info.CanPrepare = false
		info.Message = "已保存旧执行的恢复依据，可启动本机服务后核对原对话；任务结果和队列仍保留待核对状态。"
		return info, nil
	}
	if info.State == "in-use" || info.State == "unavailable" {
		return info, nil
	}
	if !cleanupWorkPending(info) && intent == nil {
		info.State = "changed"
		return info, nil
	}
	if view.boot == "" {
		view.boot, err = m.cleanupBoot()
		if err != nil {
			info.State, info.Message = "unavailable", "暂时无法核对系统重新启动的记录，原状态已保留。"
			return info, nil
		}
	}
	info.CanPrepare = false
	info.NeedsRestart = view.boot == record.RestartFrom
	for _, marker := range record.Markers {
		if marker.ID != "" && marker.BootSession == view.boot {
			info.NeedsRestart = true
		}
	}
	if info.NeedsRestart {
		info.State = "restart-required"
		info.Message = "请先重新启动这台电脑。重启后打开此面板重新检查，再确认恢复。"
	} else {
		info.State = "ready"
		info.CanApply = true
		info.Message = "已核对这台电脑在上次任务之后重新启动，可以恢复服务启动能力。"
	}
	return info, nil
}

func (m *Manager) inspectCleanup(request *CleanupRequest) (*CleanupInfo, error) {
	view, err := m.openCleanup()
	if err != nil {
		return nil, err
	}
	defer view.close()
	id := ""
	explicit := request != nil && request.ID != ""
	if explicit {
		id = request.ID
	} else {
		var pointer struct {
			ID string `json:"id"`
		}
		if err := decodeLocalJSON(filepath.Join(m.options.Root, "cleanup-current.json"), 4096, &pointer); err == nil {
			id = pointer.ID
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if id == "" {
		return &view.info, nil
	}
	record, err := m.loadCleanup(id)
	if err != nil {
		return nil, err
	}
	info, err := m.cleanupInfo(view, record)
	if !explicit && info.State == "changed" {
		return &view.info, nil
	}
	return &info, err
}

func (m *Manager) prepareCleanup(request *CleanupRequest) (*CleanupInfo, error) {
	if request == nil || !localID(request.ID) {
		return nil, errors.New("choose a stable cleanup review ID")
	}
	view, err := m.openCleanup()
	if err != nil {
		return nil, err
	}
	defer view.close()
	if record, err := m.loadCleanup(request.ID); err == nil {
		info, err := m.cleanupInfo(view, record)
		if err == nil && info.State != "changed" {
			err = m.rememberCleanup(request.ID)
		}
		return &info, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if !cleanupWorkPending(view.info) || !view.info.CanPrepare {
		return nil, errors.New("cleanup recovery cannot be prepared in the current service state")
	}
	restartFrom := ""
	for _, marker := range view.markers {
		if marker.ID == "" {
			continue
		}
		if marker.BootSession == "" || marker.BootSession == view.boot {
			restartFrom = view.boot
			break
		}
		if restartFrom == "" {
			restartFrom = marker.BootSession
		}
	}
	if restartFrom == "" {
		// A markerless restored attempt has no trusted birth proof. Checkpoint
		// this boot and require a subsequent reboot before granting recovery.
		restartFrom = view.boot
	}
	info := view.info
	info.ID, info.CreatedAt = request.ID, protocol.Now()
	record := cleanupRecord{Format: 1, Info: info, BeforeProfile: view.profile, RestartFrom: restartFrom, Markers: view.markers}
	if err = m.ensureRestoreArea("cleanup-plans"); err != nil {
		return nil, err
	}
	if err = createJSON(m.cleanupPlan(request.ID), record); err != nil {
		return nil, err
	}
	err = m.rememberCleanup(request.ID)
	if err != nil {
		return nil, err
	}
	result, err := m.cleanupInfo(view, record)
	return &result, err
}

func (m *Manager) rememberCleanup(id string) error {
	pointer := filepath.Join(m.options.Root, "cleanup-current.json")
	value := struct {
		ID string `json:"id"`
	}{id}
	if _, err := os.Lstat(pointer); errors.Is(err, os.ErrNotExist) {
		return createJSON(pointer, value)
	} else if err != nil {
		return err
	}
	return replaceJSON(pointer, value)
}

func (m *Manager) applyCleanup(ctx context.Context, request *CleanupRequest) (*CleanupInfo, error) {
	if request == nil {
		return nil, errors.New("choose an existing cleanup recovery record")
	}
	record, err := m.loadCleanup(request.ID)
	if err != nil {
		return nil, err
	}
	view, err := m.openCleanup()
	if err != nil {
		return nil, err
	}
	defer view.close()
	info, err := m.cleanupInfo(view, record)
	if err != nil {
		return nil, err
	}
	if info.Applied {
		return &info, nil
	}
	if !info.CanApply {
		return nil, errors.New("cleanup remains unconfirmed; restart this computer and review the same recovery record")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	_, _, profile, err := m.currentProfile()
	if err != nil || profile != record.BeforeProfile {
		return nil, errors.New("the device configuration changed before cleanup recovery")
	}
	intent, err := m.readCleanupIntent(record)
	if err != nil {
		return nil, err
	}
	if intent == nil {
		intent = &cleanupIntent{Format: 1, PlanSHA256: hashBytes(protocol.JSON(record)), BootSession: view.boot, State: "committing", ExecutionCapture: true}
		if err = createJSON(m.cleanupIntentPath(request.ID), intent); err != nil {
			return nil, err
		}
	}
	if intent.ExecutionCapture {
		if err = m.captureCleanupExecutions(ctx, view, record, *intent); err != nil {
			return nil, err
		}
	}
	// Capture the old attempt identities before unlocking startup. Task results,
	// queued inputs, credentials and model homes remain unchanged.
	for i, marker := range view.markers {
		if marker.ID != "" {
			if err = lifecycle.CompleteOwnership([]string{view.dirs[i]}, marker.ID); err != nil {
				return nil, err
			}
		}
	}
	intent.State = "applied"
	if err = replaceJSON(m.cleanupIntentPath(request.ID), intent); err != nil {
		return nil, err
	}
	info.State, info.Applied, info.CanApply, info.Pending, info.NeedsRestart = "applied", true, false, false, false
	info.CanPrepare = false
	info.Message = "已保存旧执行的恢复依据，可启动本机服务后核对原对话；任务结果和队列仍保留待核对状态。"
	return &info, nil
}

func (m *Manager) captureCleanupExecutions(ctx context.Context, view *cleanupView, record cleanupRecord, intent cleanupIntent) error {
	path := filepath.Join(view.dirs[1], "executor.db")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("execution recovery requires its existing regular database")
	}
	state, err := store.Open(path)
	if err != nil {
		return err
	}
	err = state.CaptureRestartRecovery(ctx, store.RestartEvidence{ID: record.Info.ID, DeviceID: view.info.DeviceID, BootSession: intent.BootSession, RestartFrom: record.RestartFrom, PlanSHA256: intent.PlanSHA256})
	return errors.Join(err, state.Close())
}
