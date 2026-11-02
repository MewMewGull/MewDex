package localnode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"mewdex/internal/backup"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
)

// A backup's profile describes owned state. It never supplies an executable,
// engine arguments, or model credentials for a later restore to run or import.
type backupProfile struct {
	Format                int           `json:"format"`
	ProtocolVersion       int           `json:"protocolVersion"`
	EngineVersion         string        `json:"engineVersion"`
	EngineHistoryIncluded bool          `json:"engineHistoryIncluded"`
	Config                config.Config `json:"config"`
}

func (m *Manager) ownedRelative(absolute string) (string, error) {
	relative, err := filepath.Rel(m.options.Root, absolute)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return "", errors.New("device state must be inside this application's data directory")
	}
	return relative, nil
}

func (m *Manager) backup(ctx context.Context, request *BackupRequest) (*BackupInfo, error) {
	if request == nil || !filepath.IsAbs(request.Path) {
		return nil, errors.New("choose an absolute destination for the device backup")
	}
	if relative, err := filepath.Rel(m.options.Root, request.Path); err == nil && (relative == "." || filepath.IsLocal(relative)) {
		return nil, errors.New("save the backup outside the application's own data directory")
	}
	c, err := config.Read(m.profile())
	if err != nil {
		return nil, err
	}
	engineVersion := engine.CompatibleVersion
	if c.Executor && c.Engine.Home != "" {
		engineVersion, err = runtimefiles.EngineVersion(c.Engine.Executable)
		if err != nil {
			return nil, errors.New("owned engine history requires its verified runtime version before export")
		}
	}
	lock, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		return nil, errors.New("stop the local device service before backing up its state")
	}
	defer lock.Close()
	if len(c.Engine.Args) != 0 || c.Engine.ProxyURL != "" || c.Engine.ProxyTokenFile != "" || (c.Gateway != nil && (c.Gateway.TLSCertFile != "" || c.Gateway.TLSKeyFile != "")) {
		return nil, errors.New("manual engine arguments, external proxy credentials and TLS key files are not supported by this device backup format yet")
	}
	data, err := m.ownedRelative(c.DataDir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(m.options.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	selected, err := dataSelections(root, data)
	if err != nil {
		return nil, err
	}
	for i := range selected {
		name, relErr := filepath.Rel(data, selected[i].Path)
		if relErr != nil {
			return nil, relErr
		}
		selected[i].Name = path.Join("data", filepath.ToSlash(name))
	}
	add := func(relative, name string) error {
		if _, err := root.Lstat(relative); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		selected = append(selected, backup.Selection{Path: relative, Name: name})
		return nil
	}
	// Include the actual MewDex stores, WAL sidecars and attachments. Runtime
	// locks/control records and unrelated directories are never archived.
	c.DataDir = "data"
	c.Engine.Executable, c.Engine.Args = "", nil
	c.Engine.ProxyUsername = c.DeviceID
	if c.NodeTokenFile != "" {
		relative, err := m.ownedRelative(c.NodeTokenFile)
		if err != nil {
			return nil, err
		}
		// An executor credential is essential; do not produce an apparently
		// complete backup with a missing network identity.
		if _, err = root.Lstat(relative); err != nil {
			return nil, err
		}
		selected = append(selected, backup.Selection{Path: relative, Name: "node-token"})
		c.NodeTokenFile = "node-token"
	}
	history := false
	if c.Executor && c.Engine.Home != "" {
		home, err := m.ownedRelative(c.Engine.Home)
		if err != nil {
			return nil, err
		}
		for _, name := range []string{"sessions", "archived_sessions"} {
			if err = add(filepath.Join(home, name), path.Join("engine-home", name)); err != nil {
				return nil, err
			}
		}
		c.Engine.Home = "engine-home"
		history = true
	} else if !c.Executor {
		c.Engine.Home = ""
	}
	metadata, err := json.Marshal(backupProfile{Format: 1, ProtocolVersion: protocol.Version, EngineVersion: engineVersion, EngineHistoryIncluded: history, Config: c})
	if err != nil {
		return nil, err
	}
	manifest, err := backup.Create(ctx, m.options.Root, request.Path, request.Passphrase, metadata, selected, backup.Options{Report: m.archiveProgress, IncludeFile: func(name string) bool {
		if strings.HasPrefix(name, "engine-home/") {
			// Pinned Codex histories are rollout JSONL files. Do not copy auth,
			// provider configuration, caches, or keychain material.
			return engine.IsHistoryBackupFile(strings.TrimPrefix(name, "engine-home/"))
		}
		return true
	}})
	if err != nil {
		return nil, err
	}
	info := &BackupInfo{Path: request.Path, DeviceID: c.DeviceID, CreatedAt: manifest.CreatedAt, Files: len(manifest.Files), EngineHistoryIncluded: history}
	for _, entry := range manifest.Files {
		info.Bytes += entry.Bytes
	}
	return info, nil
}
