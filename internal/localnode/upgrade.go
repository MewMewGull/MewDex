package localnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"mewdex/internal/backup"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/store"
)

type upgradeRecord struct {
	Format               int           `json:"format"`
	Info                 UpgradeInfo   `json:"info"`
	State                restoreRecord `json:"state"`
	ProgramSHA256        string        `json:"programSha256"`
	EngineManifestSHA256 string        `json:"engineManifestSha256,omitempty"`
	CredentialFiles      []backup.File `json:"credentialFiles"`
	ProfileFile          string        `json:"profileFile,omitempty"`
}

// UpgradeProfile exposes the same reviewed migration for an existing CLI
// configuration. Its parent owns the data and credentials. A verified shared
// runtime may be read as the old source, and the new runtime is staged locally.
// Only these upgrade operations use this alternate profile location.
func UpgradeProfile(ctx context.Context, profile, program, runtime string, request Request) (Result, error) {
	if !filepath.IsAbs(profile) || request.ExpectedDeviceID == "" {
		return Result{}, errors.New("an absolute profile and expected device identity are required")
	}
	if request.Operation != "upgrade.prepare" && request.Operation != "upgrade.apply" && request.Operation != "upgrade.status" {
		return Result{}, errors.New("an existing CLI profile supports the reviewed upgrade operations")
	}
	profile = filepath.Clean(profile)
	c, err := config.Read(profile)
	if err != nil {
		return Result{}, err
	}
	if c.DeviceID != request.ExpectedDeviceID {
		return Result{}, errors.New("the existing profile belongs to another device")
	}
	manager, err := New(Options{Root: filepath.Dir(profile), Program: program, Engine: runtime})
	if err != nil {
		return Result{}, err
	}
	manager.upgradeProfile = profile
	return manager.Handle(ctx, request)
}

func (m *Manager) upgradeDirectory(id string) string {
	return filepath.Join(m.options.Root, "upgrades", id)
}
func (m *Manager) upgradeRecord(id string) string {
	return filepath.Join(m.options.Root, "upgrade-plans", id+".json")
}

func (m *Manager) loadUpgrade(request *UpgradeRequest) (upgradeRecord, error) {
	var record upgradeRecord
	if request == nil || !localID(request.ID) {
		return record, errors.New("a stable prepared upgrade ID is required")
	}
	if err := decodeLocalJSON(m.upgradeRecord(request.ID), 32<<20, &record); err != nil {
		return record, err
	}
	if record.Format != 1 || record.Info.ID != request.ID || record.State.Info.ID != request.ID {
		return record, errors.New("invalid prepared upgrade record")
	}
	profileFile := record.ProfileFile
	if profileFile == "" {
		profileFile = "device.json"
	}
	if profileFile != filepath.Base(m.profile()) {
		return record, errors.New("prepared upgrade belongs to another profile file")
	}
	return record, nil
}

// Credentials remain at their existing paths. Only network credential files are
// fingerprinted; model authentication files and keychains are never inspected.
func (m *Manager) upgradeCredentials(ctx context.Context, c config.Config) ([]backup.File, error) {
	items := []backup.Selection{}
	seen := map[string]bool{}
	for _, file := range []string{c.NodeTokenFile, c.Engine.ProxyTokenFile} {
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		rel, err := m.ownedRelative(file)
		if err != nil {
			return nil, err
		}
		items = append(items, backup.Selection{Path: rel, Name: filepath.ToSlash(rel)})
	}
	return backup.Fingerprint(ctx, m.options.Root, items)
}

func (m *Manager) prepareUpgrade(ctx context.Context, request *UpgradeRequest) (*UpgradeInfo, error) {
	if request == nil || !localID(request.ID) {
		return nil, errors.New("a stable upgrade preparation ID is required")
	}
	if record, err := m.loadUpgrade(request); err == nil {
		_, _, digest, err := m.currentProfile()
		if err != nil {
			return nil, err
		}
		info := record.Info
		info.Applied = digest == record.State.PreparedProfile
		if !info.Applied {
			if err = m.verifyUpgradeRuntime(ctx, record); err != nil {
				return nil, err
			}
		}
		return &info, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	current, raw, before, err := m.currentProfile()
	if err != nil {
		return nil, err
	}
	if current == nil || raw == nil {
		return nil, errors.New("configure this device before preparing an upgrade")
	}
	c := *current
	if len(c.Engine.Args) != 0 {
		return nil, errors.New("custom engine arguments need an explicit migration; the existing configuration was preserved")
	}
	release, err := m.lockCurrentData(current)
	if err != nil {
		return nil, err
	}
	defer release()
	beforeData, err := m.currentData(ctx, current)
	if err != nil {
		return nil, err
	}
	credentials, err := m.upgradeCredentials(ctx, c)
	if err != nil {
		return nil, err
	}
	programHash, err := archiveFingerprint(ctx, m.options.Program)
	if err != nil {
		return nil, err
	}
	if _, err = runtimefiles.Program(m.options.Root, m.options.Program); err != nil {
		return nil, err
	}
	sourceVersion := ""
	manifestHash := ""
	if c.Executor || c.Hub {
		if m.upgradeProfile == "" {
			if _, err = m.ownedRelative(c.Engine.Executable); err != nil {
				return nil, err
			}
		}
		manifest := filepath.Join(filepath.Dir(filepath.Dir(c.Engine.Executable)), "mewdex-engine.json")
		_, manifestErr := os.Lstat(manifest)
		if m.upgradeProfile != "" && errors.Is(manifestErr, os.ErrNotExist) {
			var version string
			version, err = engine.ReadVersion(ctx, c.Engine)
			if err == nil {
				if !strings.HasPrefix(version, "codex-cli ") {
					return nil, errors.New("the existing bare runtime is not a supported Codex CLI")
				}
				sourceVersion = strings.TrimPrefix(version, "codex-cli ")
			}
		} else {
			sourceVersion, err = runtimefiles.EngineVersion(c.Engine.Executable)
		}
		if err != nil {
			return nil, err
		}
		if !engine.CanReadHistoryVersion(sourceVersion) {
			return nil, errors.New("the existing engine history needs a separate supported migration")
		}
		c.Engine.Executable, err = runtimefiles.Engine(m.options.Root, m.options.Engine, engine.CompatibleVersion)
		if err != nil {
			return nil, err
		}
		manifestHash, err = archiveFingerprint(ctx, filepath.Join(filepath.Dir(filepath.Dir(c.Engine.Executable)), "mewdex-engine.json"))
		if err != nil {
			return nil, err
		}
	}
	if err = m.ensureRestoreArea("upgrades"); err != nil {
		return nil, err
	}
	directory := m.upgradeDirectory(request.ID)
	if err = os.Mkdir(directory, 0700); err != nil {
		return nil, err
	}
	stageInfo, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if complete {
			return
		}
		// Remove only this newly created, still-identical stage. The resolved
		// absolute target is checked again before rooted recursive removal.
		canonical, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return
		}
		canonicalParent, err := filepath.EvalSymlinks(filepath.Join(m.options.Root, "upgrades"))
		if err != nil || filepath.Dir(canonical) != canonicalParent || filepath.Base(canonical) != request.ID {
			return
		}
		current, err := os.Stat(canonical)
		if err != nil || !os.SameFile(stageInfo, current) {
			return
		}
		parent, err := os.OpenRoot(canonicalParent)
		if err != nil {
			return
		}
		defer parent.Close()
		_ = parent.RemoveAll(request.ID)
	}()
	root, err := os.OpenRoot(m.options.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	relative, err := m.ownedRelative(current.DataDir)
	if err != nil {
		return nil, err
	}
	selections, err := dataSelections(root, relative)
	if err != nil {
		return nil, err
	}
	for i := range selections {
		name, err := filepath.Rel(relative, selections[i].Path)
		if err != nil {
			return nil, err
		}
		selections[i].Name = filepath.ToSlash(filepath.Join("data", name))
	}
	if _, err = backup.CopyVerified(ctx, m.options.Root, directory, selections, backup.Options{Report: m.archiveProgress}); err != nil {
		return nil, err
	}
	c.DataDir = filepath.Join(directory, "data")
	if err = os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, err
	}
	if err = createJSON(filepath.Join(directory, "device.json"), c); err != nil {
		return nil, err
	}
	loaded, err := config.Load(filepath.Join(directory, "device.json"))
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"hub.db", "executor.db", "coordinator/coordinator.db"} {
		path := filepath.Join(c.DataDir, name)
		if _, err = os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if err = store.ValidateSnapshot(ctx, path); err != nil {
			return nil, err
		}
		s, err := store.Open(path)
		if err != nil {
			return nil, err
		}
		if name == "executor.db" {
			err = executor.ValidateWorkspaceConfiguration(loaded, s)
			if err == nil {
				err = s.PrepareUpgrade(ctx, c.DeviceID)
			}
		}
		if name == "coordinator/coordinator.db" {
			err = s.ResetCoordinatorWorkspaces()
			if err == nil {
				err = s.PrepareUpgrade(ctx, c.DeviceID+".hub")
			}
		}
		closeErr := s.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	_, _, after, err := m.currentProfile()
	if err != nil {
		return nil, err
	}
	if after != before {
		return nil, errors.New("device configuration changed during upgrade preparation")
	}
	files, err := restoreFiles(ctx, directory)
	if err != nil {
		return nil, err
	}
	prepared, err := os.ReadFile(filepath.Join(directory, "device.json"))
	if err != nil {
		return nil, err
	}
	info := UpgradeInfo{ID: request.ID, DeviceID: c.DeviceID, Name: c.Name, CreatedAt: protocol.Now(), Hub: c.Hub, Executor: c.Executor, Projects: c.Projects, SourceEngineVersion: sourceVersion, TargetEngineVersion: engine.CompatibleVersion}
	record := upgradeRecord{
		Format: 1, Info: info, ProfileFile: filepath.Base(m.profile()),
		State: restoreRecord{
			Format: 1, Info: RestoreInfo{ID: request.ID, Hub: c.Hub, Executor: c.Executor},
			BeforeProfile: before, BeforeData: beforeData, PreparedProfile: hashBytes(prepared),
			Files: files, EngineVersion: engine.CompatibleVersion,
		},
		ProgramSHA256: programHash, EngineManifestSHA256: manifestHash, CredentialFiles: credentials,
	}
	if err = m.ensureRestoreArea("upgrade-plans"); err != nil {
		return nil, err
	}
	if err = createJSON(m.upgradeRecord(request.ID), record); err != nil {
		return nil, err
	}
	complete = true
	return &info, nil
}

func (m *Manager) verifyUpgradeRuntime(ctx context.Context, record upgradeRecord) error {
	programHash, err := archiveFingerprint(ctx, m.options.Program)
	if err != nil {
		return err
	}
	if record.State.EngineVersion != engine.CompatibleVersion || programHash != record.ProgramSHA256 {
		return errors.New("application changed after upgrade preparation; prepare a new upgrade ID")
	}
	if record.Info.Executor || record.Info.Hub {
		var c config.Config
		raw, err := os.ReadFile(filepath.Join(m.upgradeDirectory(record.Info.ID), "device.json"))
		if err != nil {
			return err
		}
		if json.Unmarshal(raw, &c) != nil {
			return errors.New("prepared upgrade configuration is invalid")
		}
		manifestHash, err := archiveFingerprint(ctx, filepath.Join(filepath.Dir(filepath.Dir(c.Engine.Executable)), "mewdex-engine.json"))
		if err != nil {
			return err
		}
		if manifestHash != record.EngineManifestSHA256 {
			return errors.New("prepared upgrade engine changed after review")
		}
		if _, err = runtimefiles.EngineResources(c.Engine.Executable, engine.CompatibleVersion); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) applyUpgrade(ctx context.Context, request *UpgradeRequest) (*UpgradeInfo, error) {
	record, err := m.loadUpgrade(request)
	if err != nil {
		return nil, err
	}
	current, _, digest, err := m.currentProfile()
	if err != nil {
		return nil, err
	}
	info := record.Info
	if digest == record.State.PreparedProfile {
		info.Applied = true
		return &info, nil
	}
	if current == nil {
		return nil, errors.New("current device configuration is missing")
	}
	if err = m.verifyUpgradeRuntime(ctx, record); err != nil {
		return nil, err
	}
	credentials, err := m.upgradeCredentials(ctx, *current)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(protocol.JSON(credentials), protocol.JSON(record.CredentialFiles)) {
		return nil, errors.New("network credentials changed after upgrade review; prepare a new upgrade")
	}
	if _, err = m.applyPreparedState(ctx, record.State, m.upgradeDirectory(info.ID), filepath.Join(m.options.Root, "upgrade-plans", info.ID+".previous.json")); err != nil {
		return nil, err
	}
	info.Applied = true
	return &info, nil
}

func (m *Manager) upgradeStatus(ctx context.Context, request *UpgradeRequest) (Result, error) {
	record, err := m.loadUpgrade(request)
	if err != nil {
		return Result{}, err
	}
	_, _, digest, err := m.currentProfile()
	if err != nil {
		return Result{}, err
	}
	info := record.Info
	info.Applied = digest == record.State.PreparedProfile
	snapshot, err := m.status(ctx)
	if err != nil {
		snapshot = Snapshot{State: "unreadable", Projects: []protocol.Project{}, Message: err.Error()}
	}
	return Result{Snapshot: snapshot, Upgrade: &info}, nil
}
