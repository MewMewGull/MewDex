package localnode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mewdex/internal/backup"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/store"
)

type restoreRecord struct {
	Format          int           `json:"format"`
	Info            RestoreInfo   `json:"info"`
	BeforeProfile   string        `json:"beforeProfile"`
	BeforeData      []backup.File `json:"beforeData"`
	PreparedProfile string        `json:"preparedProfile"`
	Files           []backup.File `json:"files"`
	ArchiveSHA256   string        `json:"archiveSha256"`
	EngineVersion   string        `json:"engineVersion,omitempty"`
}

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func archiveFingerprint(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("restore input must be a regular encrypted archive")
	}
	digest := sha256.New()
	buffer := make([]byte, 128<<10)
	for {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			_, _ = digest.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
func (m *Manager) currentProfile() (*config.Config, []byte, string, error) {
	info, err := os.Lstat(m.profile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, "", nil
	}
	if err != nil {
		return nil, nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, "", errors.New("current profile must be a regular file")
	}
	raw, err := os.ReadFile(m.profile())
	if err != nil {
		return nil, nil, "", err
	}
	c, readErr := config.Read(m.profile())
	if readErr != nil {
		return nil, raw, hashBytes(raw), nil
	}
	return &c, raw, hashBytes(raw), nil
}

func (m *Manager) dataDirectories(c *config.Config, exclude string) ([]string, error) {
	paths := []string{filepath.Join(m.options.Root, "data")}
	if c != nil {
		paths = []string{c.DataDir}
	} else {
		for _, area := range []string{"restores", "upgrades"} {
			entries, err := os.ReadDir(filepath.Join(m.options.Root, area))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if len(entries) > 1024 {
				return nil, errors.New("too many retained restore directories; inspect them before recovery")
			}
			for _, entry := range entries {
				if entry.IsDir() && localID(entry.Name()) && entry.Name() != exclude {
					paths = append(paths, filepath.Join(m.options.Root, area, entry.Name(), "data"))
				}
			}
		}
	}
	return paths, nil
}

func (m *Manager) lockCurrentData(c *config.Config, exclude ...string) (func(), error) {
	skip := ""
	if len(exclude) > 0 {
		skip = exclude[0]
	}
	paths, err := m.dataDirectories(c, skip)
	if err != nil {
		return nil, err
	}
	locks := []*lifecycle.Lock{}
	release := func() {
		for _, lock := range locks {
			lock.Close()
		}
	}
	for _, path := range paths {
		if _, err := m.ownedRelative(path); err != nil {
			release()
			return nil, err
		}
		lock, err := lifecycle.Acquire(path)
		if err != nil {
			release()
			return nil, errors.New("stop the device service before preparing or applying a restore")
		}
		locks = append(locks, lock)
	}
	return release, nil
}

func (m *Manager) currentData(ctx context.Context, c *config.Config, exclude ...string) ([]backup.File, error) {
	skip := ""
	if len(exclude) > 0 {
		skip = exclude[0]
	}
	paths, err := m.dataDirectories(c, skip)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(m.options.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	selected := []backup.Selection{}
	for _, path := range paths {
		relative, err := m.ownedRelative(path)
		if err != nil {
			return nil, err
		}
		items, err := dataSelections(root, relative)
		if err != nil {
			return nil, err
		}
		selected = append(selected, items...)
	}
	return backup.Fingerprint(ctx, m.options.Root, selected)
}

func restoreFiles(ctx context.Context, directory string) ([]backup.File, error) {
	return backup.InventoryFiltered(ctx, directory, backup.Options{Exclude: func(name string) bool { return name == "data/service.lock" }})
}

func dataSelections(root *os.Root, relative string) ([]backup.Selection, error) {
	selected := []backup.Selection{}
	for _, name := range []string{"hub.db", "hub.db-wal", "hub.db-shm", "executor.db", "executor.db-wal", "executor.db-shm", "attachments", "terminal-attachments", "coordinator/coordinator.db", "coordinator/coordinator.db-wal", "coordinator/coordinator.db-shm", "coordinator/attachments"} {
		path := filepath.Join(relative, name)
		if _, err := root.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		selected = append(selected, backup.Selection{Path: path, Name: filepath.ToSlash(path)})
	}
	return selected, nil
}

func (m *Manager) restoreDirectory(id string) string {
	return filepath.Join(m.options.Root, "restores", id)
}
func (m *Manager) restoreRecord(id string) string {
	return filepath.Join(m.options.Root, "restore-plans", id+".json")
}
func (m *Manager) ensureRestoreArea(name string) error {
	root, err := os.OpenRoot(m.options.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.MkdirAll(name, 0700); err != nil {
		return err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("restore area must be an owned directory, not a link")
	}
	return nil
}
func (m *Manager) loadRestore(request *RestoreRequest) (restoreRecord, error) {
	var record restoreRecord
	if request == nil || !localID(request.ID) {
		return record, errors.New("a valid prepared restore ID is required")
	}
	file, err := os.Open(m.restoreRecord(request.ID))
	if err != nil {
		return record, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 32<<20+1))
	if err != nil || len(raw) > 32<<20 {
		return record, errors.New("restore plan is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || record.Format != 1 || record.Info.ID != request.ID {
		return record, errors.New("invalid prepared restore record")
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return record, errors.New("unexpected restore record contents")
	}
	return record, nil
}

func validateBackupProfile(manifest backup.Manifest) (backupProfile, error) {
	var profile backupProfile
	decoder := json.NewDecoder(bytes.NewReader(manifest.Metadata))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&profile) != nil || profile.Format != 1 || profile.ProtocolVersion != protocol.Version || !engine.CanReadHistoryVersion(profile.EngineVersion) {
		return profile, errors.New("unsupported device backup or engine version")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return profile, errors.New("unexpected device metadata")
	}
	c := profile.Config
	if c.DeviceID == "" || len(c.DeviceID) > 128 || strings.TrimSpace(c.Name) == "" || c.DataDir != "data" || (!c.Hub && !c.Executor && c.Gateway == nil) || c.Engine.Executable != "" || len(c.Engine.Args) != 0 || c.Engine.ProxyURL != "" || c.Engine.ProxyTokenFile != "" || (c.Engine.Home != "" && c.Engine.Home != "engine-home") || (c.NodeTokenFile != "" && c.NodeTokenFile != "node-token") {
		return profile, errors.New("backup profile contains invalid roles, host paths or executable settings")
	}
	if profile.EngineHistoryIncluded != ((c.Hub || c.Executor) && c.Engine.Home == "engine-home") {
		return profile, errors.New("backup history scope does not match its device profile")
	}
	if c.Gateway != nil && (c.Gateway.TLSCertFile != "" || c.Gateway.TLSKeyFile != "") {
		return profile, errors.New("external TLS key files cannot be adopted from a backup")
	}
	for _, entry := range manifest.Files {
		name := entry.Name
		if name == "node-token" {
			if c.NodeTokenFile == "" {
				return profile, errors.New("undeclared executor identity")
			}
			continue
		}
		if strings.HasPrefix(name, "engine-home/") && profile.EngineHistoryIncluded && engine.IsHistoryBackupFile(strings.TrimPrefix(name, "engine-home/")) {
			continue
		}
		if strings.HasPrefix(name, "data/attachments/") {
			continue
		}
		if c.Hub && strings.HasPrefix(name, "data/coordinator/attachments/") {
			continue
		}
		if c.Executor && strings.HasPrefix(name, "data/terminal-attachments/") {
			continue
		}
		switch name {
		case "data/coordinator/coordinator.db", "data/coordinator/coordinator.db-wal", "data/coordinator/coordinator.db-shm":
			if !c.Hub {
				return profile, errors.New("unexpected Hub conversation database")
			}
		case "data/hub.db", "data/hub.db-wal", "data/hub.db-shm":
			if !c.Hub {
				return profile, errors.New("unexpected Hub database")
			}
		case "data/executor.db", "data/executor.db-wal", "data/executor.db-shm":
			if !c.Executor {
				return profile, errors.New("unexpected executor database")
			}
		default:
			return profile, errors.New("backup contains data outside the managed device layout")
		}
	}
	for _, project := range c.Projects {
		if !filepath.IsAbs(project.Path) {
			return profile, errors.New("restore requires the original absolute project directory bindings")
		}
	}
	return profile, nil
}

func (m *Manager) prepareRestore(ctx context.Context, request *RestoreRequest) (*RestoreInfo, error) {
	m.report("checking-current")
	if request == nil || !filepath.IsAbs(request.Path) {
		return nil, errors.New("choose an absolute backup file path")
	}
	id := request.ID
	if id == "" {
		id = protocol.ID()
	} else if !localID(id) {
		return nil, errors.New("restore preparation ID must be a stable lowercase hexadecimal identifier")
	}
	if _, statErr := os.Stat(m.restoreRecord(id)); statErr == nil {
		archiveSHA, err := archiveFingerprint(ctx, request.Path)
		if err != nil {
			return nil, err
		}
		record, err := m.loadRestore(&RestoreRequest{ID: id})
		if err != nil {
			return nil, err
		}
		if record.ArchiveSHA256 != archiveSHA {
			return nil, errors.New("restore preparation ID already belongs to a different archive")
		}
		_, _, current, err := m.currentProfile()
		if err != nil {
			return nil, err
		}
		info := record.Info
		info.Applied = current == record.PreparedProfile
		if !info.Applied && info.Executor && record.EngineVersion != engine.CompatibleVersion {
			return nil, errors.New("prepared restore belongs to another engine version; prepare a new restore ID with this application")
		}
		return &info, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	current, raw, before, err := m.currentProfile()
	if err != nil {
		return nil, err
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
	parent := filepath.Join(m.options.Root, "restores")
	if err = m.ensureRestoreArea("restores"); err != nil {
		return nil, err
	}
	directory := m.restoreDirectory(id)
	input, err := os.Open(request.Path)
	if err != nil {
		return nil, err
	}
	inputInfo, err := input.Stat()
	if err != nil || !inputInfo.Mode().IsRegular() {
		input.Close()
		return nil, errors.New("restore input must be a regular encrypted archive")
	}
	digest := sha256.New()
	manifest, err := backup.ExtractReaderWithOptions(ctx, io.TeeReader(input, digest), request.Passphrase, parent, id, backup.Options{Report: m.archiveProgress})
	input.Close()
	if err != nil {
		return nil, err
	}
	archiveSHA := hex.EncodeToString(digest.Sum(nil))
	complete := false
	defer func() {
		if !complete {
			if root, err := os.OpenRoot(m.options.Root); err == nil {
				_ = root.RemoveAll(filepath.Join("restores", id))
				root.Close()
			}
		}
	}()
	m.report("validating")
	profile, err := validateBackupProfile(manifest)
	if err != nil {
		return nil, err
	}
	c := profile.Config
	if current != nil && current.DeviceID != c.DeviceID {
		return nil, errors.New("this backup belongs to another device; the current installation was preserved")
	}
	c.DataDir = filepath.Join(directory, "data")
	if err = os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, err
	}
	if c.Executor || c.Hub {
		m.report("preparing-engine")
		c.Engine.Executable, err = runtimefiles.Engine(m.options.Root, m.options.Engine, engine.CompatibleVersion)
		if err != nil {
			return nil, err
		}
		if c.Engine.Home == "" {
			if current == nil || !current.Executor || current.Engine.Home != "" {
				return nil, errors.New("backup omitted an external engine history; restore it on its original configured device")
			}
		} else {
			c.Engine.Home = filepath.Join(directory, "engine-home")
			if err = os.MkdirAll(c.Engine.Home, 0700); err != nil {
				return nil, err
			}
		}
	}
	if c.NodeTokenFile != "" || (!c.Hub && c.Executor) {
		c.NodeTokenFile = filepath.Join(directory, "node-token")
	}
	// Keep a network identity for historical correlation, but never activate a
	// token from an older snapshot. A restored executor must pair again.
	if err = os.Remove(filepath.Join(directory, "node-token")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err = createJSON(filepath.Join(directory, "device.json"), c); err != nil {
		return nil, err
	}
	c, err = config.Load(filepath.Join(directory, "device.json"))
	if err != nil {
		return nil, err
	}
	if err = replaceJSON(filepath.Join(directory, "device.json"), c); err != nil {
		return nil, err
	}
	m.report("preparing-state")
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
			err = executor.ValidateWorkspaceConfiguration(c, s)
		}
		if err == nil {
			device := c.DeviceID
			if name == "coordinator/coordinator.db" {
				device += ".hub"
				err = s.ResetCoordinatorWorkspaces()
			}
			if err == nil {
				err = s.PrepareRestore(ctx, device, name != "hub.db")
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
	_, _, now, err := m.currentProfile()
	if err != nil {
		return nil, err
	}
	if now != before {
		return nil, errors.New("current configuration changed while preparing the restore")
	}
	m.report("checking-prepared")
	files, err := restoreFiles(ctx, directory)
	if err != nil {
		return nil, err
	}
	prepared, err := os.ReadFile(filepath.Join(directory, "device.json"))
	if err != nil {
		return nil, err
	}
	info := RestoreInfo{ID: id, DeviceID: c.DeviceID, Name: c.Name, CreatedAt: protocol.Now(), Hub: c.Hub, Executor: c.Executor, Projects: append([]protocol.Project{}, c.Projects...), RequiresPairing: true, RequiresModelLogin: c.Hub && c.Engine.Home != "", PreviousStatePreserved: raw != nil}
	record := restoreRecord{Format: 1, Info: info, BeforeProfile: before, BeforeData: beforeData, PreparedProfile: hashBytes(prepared), Files: files, ArchiveSHA256: archiveSHA, EngineVersion: engine.CompatibleVersion}
	if err = m.ensureRestoreArea("restore-plans"); err != nil {
		return nil, err
	}
	if err = createJSON(m.restoreRecord(id), record); err != nil {
		return nil, err
	}
	complete = true
	return &info, nil
}

func (m *Manager) applyRestore(ctx context.Context, request *RestoreRequest) (*RestoreInfo, error) {
	m.report("checking-current")
	record, err := m.loadRestore(request)
	if err != nil {
		return nil, err
	}
	return m.applyPreparedState(ctx, record, m.restoreDirectory(record.Info.ID), filepath.Join(m.options.Root, "restore-plans", record.Info.ID+".previous.json"))
}

// Restoration and upgrades share the final compare-and-switch path. All
// domain-specific migrations happen on the staged copy before user review.
func (m *Manager) applyPreparedState(ctx context.Context, record restoreRecord, directory, retained string) (*RestoreInfo, error) {
	current, previous, digest, err := m.currentProfile()
	if err != nil {
		return nil, err
	}
	if digest == record.PreparedProfile {
		info := record.Info
		info.Applied = true
		return &info, nil
	}
	if (record.Info.Executor || record.Info.Hub) && record.EngineVersion != engine.CompatibleVersion {
		return nil, errors.New("prepared restore belongs to another engine version; prepare a new restore ID with this application")
	}
	if digest != record.BeforeProfile {
		return nil, errors.New("current configuration changed; prepare the restore again before switching")
	}
	release, err := m.lockCurrentData(current, record.Info.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	data, err := m.currentData(ctx, current, record.Info.ID)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(protocol.JSON(data), protocol.JSON(record.BeforeData)) {
		return nil, errors.New("device data changed after review; prepare the restore again")
	}
	stagedLease, err := lifecycle.Acquire(filepath.Join(directory, "data"))
	if err != nil {
		return nil, errors.New("prepared restore data is in use")
	}
	defer stagedLease.Close()
	m.report("checking-prepared")
	files, err := restoreFiles(ctx, directory)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(protocol.JSON(files), protocol.JSON(record.Files)) {
		return nil, errors.New("prepared restore files changed; the current state was preserved")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "device.json"))
	if err != nil {
		return nil, err
	}
	if hashBytes(raw) != record.PreparedProfile {
		return nil, errors.New("prepared profile changed")
	}
	loaded, err := config.Load(filepath.Join(directory, "device.json"))
	if err != nil {
		return nil, err
	}
	var recorded config.Config
	if json.Unmarshal(raw, &recorded) != nil || !bytes.Equal(protocol.JSON(loaded), protocol.JSON(recorded)) {
		return nil, errors.New("prepared configuration or project bindings changed after review")
	}
	if previous != nil {
		if value, readErr := os.ReadFile(retained); readErr == nil {
			if !bytes.Equal(value, previous) {
				return nil, errors.New("retained previous profile does not match")
			}
		} else if errors.Is(readErr, os.ErrNotExist) {
			if err = createFile(retained, previous); err != nil {
				return nil, err
			}
		} else {
			return nil, readErr
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	m.report("committing")
	if err = m.adoptProfile(raw, record.BeforeProfile); err != nil {
		return nil, err
	}
	info := record.Info
	info.Applied = true
	return &info, nil
}

func (m *Manager) adoptProfile(raw []byte, expected string) error {
	_, _, actual, err := m.currentProfile()
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("current profile changed before the final restore switch")
	}
	temporary := m.profile() + "." + protocol.ID() + ".tmp"
	if err = createFile(temporary, raw); err != nil {
		return err
	}
	defer os.Remove(temporary)
	_, _, actual, err = m.currentProfile()
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("current profile changed during the final restore switch")
	}
	if expected == "" {
		return os.Link(temporary, m.profile())
	}
	return os.Rename(temporary, m.profile())
}

func (m *Manager) restoreStatus(ctx context.Context, request *RestoreRequest) (Result, error) {
	record, err := m.loadRestore(request)
	if err != nil {
		return Result{}, err
	}
	_, _, digest, err := m.currentProfile()
	if err != nil {
		return Result{}, err
	}
	info := record.Info
	info.Applied = digest == record.PreparedProfile
	snapshot, statusErr := m.status(ctx)
	if statusErr != nil {
		snapshot = Snapshot{State: "unreadable", Projects: []protocol.Project{}, Message: statusErr.Error()}
	}
	return Result{Snapshot: snapshot, Restore: &info}, nil
}
