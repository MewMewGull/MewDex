package localnode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"mewdex/internal/backup"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/hubproxy"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/startup"
	"mewdex/internal/store"
	"mewdex/internal/webui"
	"mewdex/internal/workspace"
)

type Options struct {
	Root           string
	Program        string
	Engine         string
	Report         func(OperationProgress)
	StartupBackend startup.Backend
	// BootSession is a trusted platform seam for deterministic lifecycle checks;
	// it is never accepted from a desktop or network request.
	BootSession func() (string, error)
	// SSHHome is a trusted host/test override, never accepted from a request.
	SSHHome string
}

type Manager struct{ options Options }

func New(options Options) (*Manager, error) {
	if options.Root == "" || options.Program == "" {
		return nil, errors.New("local service root and executable are required")
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return nil, err
	}
	program, err := filepath.Abs(options.Program)
	if err != nil {
		return nil, err
	}
	options.Root, options.Program = root, program
	return &Manager{options: options}, nil
}

func (m *Manager) profile() string { return filepath.Join(m.options.Root, "device.json") }
func (m *Manager) draft() string   { return filepath.Join(m.options.Root, "device.setup.json") }

func (m *Manager) Handle(ctx context.Context, request Request) (Result, error) {
	if request.Operation == "proxy.ssh.config" {
		c, err := config.Read(m.profile())
		if err != nil {
			return Result{}, err
		}
		if !c.Hub || request.ExpectedDeviceID == "" || request.ExpectedDeviceID != c.DeviceID {
			return Result{}, errors.New("请先刷新当前 Hub，再读取本机 SSH 配置")
		}
		home := m.options.SSHHome
		if home == "" {
			home, err = os.UserHomeDir()
			if err != nil {
				return Result{}, errors.New("无法确定本机用户目录")
			}
		}
		sshConfig, err := hubproxy.ReadSSHConfig(home)
		if err != nil {
			return Result{}, err
		}
		snapshot, err := m.status(ctx)
		return Result{Snapshot: snapshot, SSHConfig: &sshConfig}, err
	}
	if request.Operation == "hub.discover" {
		c, err := config.Read(m.profile())
		if err != nil {
			return Result{}, err
		}
		info := discoverHub(ctx, c)
		snapshot, err := m.status(ctx)
		return Result{Snapshot: snapshot, ExistingHub: info}, err
	}
	if request.Operation == "proxy.status" {
		info, err := m.proxyStatus(ctx)
		if err != nil {
			return Result{}, err
		}
		snapshot, err := m.status(ctx)
		return Result{Snapshot: snapshot, HubProxy: info}, err
	}
	if request.Operation == "project.location" {
		c, err := config.Read(m.profile())
		if err != nil {
			return Result{}, err
		}
		if request.ExpectedDeviceID == "" || request.ExpectedDeviceID != c.DeviceID {
			return Result{}, errors.New("this project belongs to another device")
		}
		database := filepath.Join(c.DataDir, "executor.db")
		if _, err := os.Stat(database); err == nil {
			s, err := store.Open(database)
			if err != nil {
				return Result{}, err
			}
			projects, err := store.List[protocol.Project](s, "workspaces")
			s.Close()
			if err != nil {
				return Result{}, err
			}
			c.Projects = append(c.Projects, projects...)
		}
		for _, project := range c.Projects {
			if project.ID == request.ProjectID {
				project.Path, err = workspace.CanonicalDir(project.Path)
				if err != nil {
					return Result{}, err
				}
				snapshot, err := m.status(ctx)
				return Result{Snapshot: snapshot, Project: &project}, err
			}
		}
		return Result{}, errors.New("project is not configured on this computer")
	}
	if request.Operation == "cleanup.status" {
		info, err := m.inspectCleanup(request.Cleanup)
		if err != nil {
			return Result{}, err
		}
		snapshot, err := m.status(ctx)
		return Result{Snapshot: snapshot, Cleanup: info}, err
	}
	if request.Operation == "startup.status" {
		info, err := m.startupStatus(ctx)
		if err != nil {
			return Result{}, err
		}
		snapshot, err := m.status(ctx)
		return Result{Snapshot: snapshot, Startup: &info}, err
	}
	if strings.HasPrefix(request.Operation, "job.") {
		return m.handleJob(ctx, request)
	}
	if request.Operation == "restore.status" {
		return m.restoreStatus(ctx, request.Restore)
	}
	if request.Operation == "upgrade.status" {
		return m.upgradeStatus(ctx, request.Upgrade)
	}
	if request.Operation == "status" {
		snapshot, err := m.status(ctx)
		return Result{Snapshot: snapshot}, err
	}
	lock, err := lifecycle.Acquire(filepath.Join(m.options.Root, "management"))
	if err != nil {
		return Result{}, errors.New("another local service operation is in progress")
	}
	defer lock.Close()
	if request.Operation == "service.port" && request.ExpectedDeviceID == "" {
		return Result{}, errors.New("请先刷新当前设备，再修改监听端口")
	}
	if (request.Operation == "cleanup.prepare" || request.Operation == "cleanup.apply") && request.ExpectedDeviceID == "" {
		return Result{}, errors.New("refresh this device before preparing its cleanup recovery")
	}
	var installation *lifecycle.Lock
	if request.Operation == "setup" || request.Operation == "projects" || request.Operation == "service.port" || request.Operation == "backup" || request.Operation == "restore.prepare" || request.Operation == "restore.apply" || request.Operation == "repair.executor" || request.Operation == "upgrade.prepare" || request.Operation == "upgrade.apply" {
		installation, err = lifecycle.Installation(m.options.Root)
		if err != nil {
			if request.Operation == "service.port" {
				return Result{}, errors.New("请先停止本机服务，再修改监听端口")
			}
			return Result{}, errors.New("stop the local installation before changing its state")
		}
		defer func() {
			if installation != nil {
				installation.Close()
			}
		}()
	}
	if request.ExpectedDeviceID != "" {
		c, err := config.Read(m.profile())
		if err != nil {
			return Result{}, err
		}
		if c.DeviceID != request.ExpectedDeviceID {
			return Result{}, errors.New("local device changed; refresh its settings")
		}
	}
	var backupInfo *BackupInfo
	var restoreInfo *RestoreInfo
	var startupInfo *StartupInfo
	var proxyInfo *HubProxyInfo
	var upgradeInfo *UpgradeInfo
	var cleanupInfo *CleanupInfo
	var discovered *HubDiscovery
	switch request.Operation {
	case "cleanup.prepare":
		cleanupInfo, err = m.prepareCleanup(request.Cleanup)
	case "cleanup.apply":
		cleanupInfo, err = m.applyCleanup(ctx, request.Cleanup)
	case "setup":
		err = m.setup(ctx, request.Setup)
	case "start":
		err = m.start(ctx)
		var existing *existingHubError
		if errors.As(err, &existing) {
			discovered = existing.Hub
			err = nil
		}
	case "stop":
		err = m.stop(ctx, request.ExpectedInstanceID)
	case "service.update":
		err = m.updateService(ctx, request.ExpectedInstanceID)
	case "service.port":
		err = m.applyListener(ctx, request.Listener)
	case "web.apply":
		if request.ExpectedDeviceID == "" || request.WebWorkbench == nil {
			return Result{}, errors.New("请先刷新当前 Hub，再更改网页版设置")
		}
		c, readErr := config.Read(m.profile())
		if readErr != nil {
			return Result{}, readErr
		}
		if !c.Hub {
			return Result{}, errors.New("网页版由中央后端提供")
		}
		settingsPath := filepath.Join(m.options.Root, webui.SettingsFile)
		err = createJSON(settingsPath, request.WebWorkbench)
		if errors.Is(err, os.ErrExist) {
			err = replaceJSON(settingsPath, request.WebWorkbench)
		}
	case "projects":
		err = m.projects(request.Projects)
	case "backup":
		backupInfo, err = m.backup(ctx, request.Backup)
	case "restore.prepare":
		restoreInfo, err = m.prepareRestore(ctx, request.Restore)
	case "restore.apply":
		restoreInfo, err = m.applyRestore(ctx, request.Restore)
	case "repair.executor":
		err = m.repairExecutor(ctx, request.Pairing)
	case "upgrade.prepare":
		upgradeInfo, err = m.prepareUpgrade(ctx, request.Upgrade)
	case "upgrade.apply":
		upgradeInfo, err = m.applyUpgrade(ctx, request.Upgrade)
	case "startup.apply":
		startupInfo, err = m.applyStartup(ctx, request.Startup)
	case "proxy.apply", "proxy.import":
		if request.ExpectedDeviceID == "" {
			return Result{}, errors.New("请先刷新当前 Hub")
		}
		if request.Operation == "proxy.import" {
			proxyInfo, err = m.importProxy(ctx)
		} else {
			proxyInfo, err = m.applyProxy(ctx, request.HubProxy)
		}
	case "connect":
		return m.connect(ctx)
	default:
		return Result{}, errors.New("unsupported local service operation")
	}
	if err != nil {
		return Result{}, err
	}
	if installation != nil {
		installation.Close()
		installation = nil
	}
	snapshot, err := m.status(ctx)
	if err != nil && (restoreInfo != nil || upgradeInfo != nil) {
		snapshot = Snapshot{State: "unreadable", Projects: []protocol.Project{}, Message: err.Error()}
		err = nil
	}
	result := Result{Snapshot: snapshot, Backup: backupInfo, Restore: restoreInfo, Startup: startupInfo, HubProxy: proxyInfo, ExistingHub: discovered, Upgrade: upgradeInfo, Cleanup: cleanupInfo}
	if request.Operation == "web.apply" {
		result.WebWorkbench = request.WebWorkbench
	}
	return result, err
}

func (m *Manager) report(phase string) {
	if m.options.Report != nil {
		m.options.Report(OperationProgress{Phase: phase})
	}
}

func (m *Manager) archiveProgress(value backup.Progress) {
	if m.options.Report != nil {
		m.options.Report(OperationProgress{Phase: value.Phase, Files: value.Files, TotalFiles: value.TotalFiles, Bytes: value.Bytes, TotalBytes: value.TotalBytes})
	}
}

func (m *Manager) status(ctx context.Context) (snapshot Snapshot, resultErr error) {
	defer func() {
		snapshot.StartupPreference, _ = m.StartupPreference()
		snapshot.Operations = []string{"service.update", "service.port", "web.apply", "hub.discover", "proxy.status", "proxy.apply", "proxy.import", "proxy.ssh.config", "backup", "restore.prepare", "restore.apply", "restore.status", "upgrade.prepare", "upgrade.apply", "upgrade.status", "repair.executor", "startup.status", "startup.apply", "job.start", "job.status", "job.cancel", "cleanup.status", "cleanup.prepare", "cleanup.apply"}
	}()
	snapshot = Snapshot{State: "unconfigured", Projects: []protocol.Project{}}
	c, err := config.Read(m.profile())
	if errors.Is(err, os.ErrNotExist) {
		if pending, pendingErr := config.Read(m.draft()); pendingErr == nil {
			snapshot = m.describe(pending)
			snapshot.Configured = false
			snapshot.SetupPending = true
			snapshot.State = "setup-pending"
		}
		if m.options.Engine != "" {
			_, statErr := os.Stat(m.options.Engine)
			snapshot.EnginePresent = statErr == nil
		}
		if probe, leaseErr := lifecycle.Installation(m.options.Root); leaseErr != nil {
			snapshot.State = "unknown"
			snapshot.Message = "installation is in use; its missing profile was not replaced"
		} else {
			probe.Close()
		}
		return snapshot, nil
	}
	if err != nil {
		return snapshot, errors.New("local device configuration cannot be read")
	}
	snapshot = m.describe(c)
	record, err := lifecycle.Read(c.DataDir)
	if err == nil {
		if record.DeviceID != c.DeviceID {
			return snapshot, errors.New("local control record belongs to a different device")
		}
		state, callErr := lifecycle.Call(ctx, record, false)
		if callErr == nil {
			snapshot.State, _ = state["state"].(string)
			snapshot.InstanceID = record.InstanceID
			snapshot.Ready = snapshot.State == "running"
			if ready, exists := state["ready"].(bool); exists {
				snapshot.Ready = ready
			}
			snapshot.BaseURL, _ = state["hubUrl"].(string)
			if c.Hub && snapshot.BaseURL == "" {
				snapshot.BaseURL = listenURL(c.Listen)
			}
			return snapshot, nil
		}
	}
	probe, lockErr := lifecycle.Acquire(c.DataDir)
	if lockErr != nil {
		snapshot.State = "unknown"
		snapshot.Message = "local service owns its data directory, but its control channel is unavailable"
		if errors.Is(lockErr, lifecycle.ErrUnconfirmed) {
			snapshot.Message = "previous service cleanup is unconfirmed; review its recovery before restarting"
		}
		return snapshot, nil
	}
	probe.Close()
	installation, leaseErr := lifecycle.Installation(m.options.Root)
	if leaseErr != nil {
		snapshot.State = "unknown"
		snapshot.Message = "installation is in use; no stopped state was assumed"
		if errors.Is(leaseErr, lifecycle.ErrUnconfirmed) {
			snapshot.Message = "previous installation cleanup is unconfirmed; review its recovery before restarting"
		}
		return snapshot, nil
	}
	installation.Close()
	snapshot.State = "stopped"
	return snapshot, nil
}

func (m *Manager) describe(c config.Config) Snapshot {
	_, _, err := engine.Resolve(c.Engine)
	if err == nil && c.Engine.Executable != "" {
		info, statErr := os.Stat(c.Engine.Executable)
		if statErr != nil || !info.Mode().IsRegular() {
			err = errors.New("configured execution engine is missing")
		}
	}
	projects := append([]protocol.Project{}, c.Projects...)
	pairingRequired := false
	if c.Executor && !c.Hub {
		pairingRequired = m.executorNeedsPairing(c)
	}
	snapshot := Snapshot{Configured: true, State: "stopped", DeviceID: c.DeviceID, Name: c.Name, Hub: c.Hub, Executor: c.Executor, Gateway: c.Gateway != nil, HubURL: c.HubURL, Listen: c.Listen, UseHubGateway: c.Engine.UseHubGateway, Projects: projects, EnginePresent: err == nil, ExistingModelAccount: c.Engine.Home == "", PairingRequired: pairingRequired}
	if c.Hub {
		snapshot.BaseURL = listenURL(c.Listen)
	}
	return snapshot
}
