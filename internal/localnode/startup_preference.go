package localnode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
)

// Installers save an explicit choice even before there is a device identity.
// It becomes an OS registration only after local setup has succeeded.
func (m *Manager) StartupPreference() (*bool, error) {
	raw, err := os.ReadFile(filepath.Join(m.options.Root, "startup-preference.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var request StartupRequest
	if len(raw) > 1024 || json.Unmarshal(raw, &request) != nil {
		return nil, errors.New("无法读取安装时的自动启动选择")
	}
	return &request.Enabled, nil
}

func (m *Manager) SaveStartupPreference(ctx context.Context, enabled bool) error {
	lock, err := lifecycle.Acquire(filepath.Join(m.options.Root, "management"))
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = m.saveStartupPreference(enabled); err != nil {
		return err
	}
	if _, err = os.Stat(m.profile()); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	_, err = m.applyStartup(ctx, &StartupRequest{Enabled: enabled})
	return err
}

func (m *Manager) saveStartupPreference(enabled bool) error {
	if err := os.MkdirAll(m.options.Root, 0700); err != nil {
		return err
	}
	tmp := filepath.Join(m.options.Root, "startup-preference."+protocol.ID()+".tmp")
	defer os.Remove(tmp)
	if err := os.WriteFile(tmp, protocol.JSON(StartupRequest{Enabled: enabled}), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(m.options.Root, "startup-preference.json"))
}
