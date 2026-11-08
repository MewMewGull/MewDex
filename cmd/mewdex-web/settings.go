package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/startup"
)

type webSettings struct {
	ID               string `json:"id"`
	HubURL           string `json:"hubURL"`
	Listen           string `json:"listen"`
	Directory        string `json:"directory"`
	AllowPrivateHTTP bool   `json:"allowPrivateHTTP,omitempty"`
}

func saveSettings(root string, value webSettings) error {
	path := filepath.Join(root, "web.json")
	if err := os.WriteFile(path+".tmp", protocol.JSON(value), 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func webStartup(root string) (*startup.Controller, error) {
	backend, err := startup.Native()
	if err != nil {
		return nil, err
	}
	return startup.New(root, backend)
}

func webBinding(root string) (startup.Binding, error) {
	raw, err := os.ReadFile(filepath.Join(root, "web.json"))
	if err != nil {
		return startup.Binding{}, err
	}
	var value webSettings
	if json.Unmarshal(raw, &value) != nil || value.ID == "" {
		return startup.Binding{}, errors.New("invalid web input identity")
	}
	return startup.Binding{DeviceID: value.ID, ProfileSHA256: startup.Fingerprint(raw)}, nil
}

func (h *webHost) applyStartup(ctx context.Context, enabled bool) error {
	controller, err := webStartup(h.root)
	if err != nil {
		return err
	}
	program := ""
	if enabled {
		program, err = os.Executable()
		if err != nil {
			return err
		}
		program, err = runtimefiles.Program(h.root, program)
		if err != nil {
			return err
		}
		// UI-only upgrades can share the same Go binary, so bind assets by their own content.
		h.mu.Lock()
		defer h.mu.Unlock()
		destination, stageErr := stageUI(h.root, h.settings.Directory)
		if stageErr != nil {
			return stageErr
		}
		settings := h.settings
		settings.Directory = destination
		if err = saveSettings(h.root, settings); err != nil {
			return err
		}
		h.settings = settings
	}
	binding, err := webBinding(h.root)
	if err != nil {
		return err
	}
	_, err = controller.Apply(ctx, enabled, program, binding)
	return err
}

func startupEnabled(root string) bool {
	var record startup.Record
	raw, err := os.ReadFile(filepath.Join(root, "startup.json"))
	return err == nil && json.Unmarshal(raw, &record) == nil && record.Enabled
}

func readSettings(root string) (webSettings, error) {
	raw, err := os.ReadFile(filepath.Join(root, "web.json"))
	if err != nil {
		return webSettings{}, err
	}
	var value webSettings
	if len(raw) > 64<<10 || json.Unmarshal(raw, &value) != nil || value.ID == "" || !filepath.IsAbs(value.Directory) {
		return value, fs.ErrInvalid
	}
	return value, nil
}
