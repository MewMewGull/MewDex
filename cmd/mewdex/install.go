package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"time"

	"mewdex/internal/backhaul"
	"mewdex/internal/localnode"
	"mewdex/internal/protocol"
	"mewdex/internal/startup"
)

// The installer calls the same manager as the workbench. The selected data
// directory is respected; upgrades never create a second device installation.
func install(args []string) error {
	f := flag.NewFlagSet("install", flag.ContinueOnError)
	root := f.String("root", "", "default local data directory")
	appConfig := f.String("app-config", "", "desktop preferences directory")
	workbench := f.String("workbench", "", "installed workbench executable")
	autostart := f.Bool("autostart", false, "explicit installer login startup choice")
	uninstall := f.Bool("uninstall", false, "remove startup entry and stop the owned service")
	inspect := f.String("inspect-output", "", "write existing startup choice as an installer INI file")
	prepareUpdate := f.Bool("prepare-update", false, "check running work before replacing application files")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !filepath.IsAbs(*root) || !filepath.IsAbs(*appConfig) {
		return errors.New("installer requires absolute data and preferences paths")
	}
	raw, err := os.ReadFile(filepath.Join(*appConfig, "local-installation.json"))
	if err == nil {
		var selected struct {
			Root string `json:"root"`
		}
		if len(raw) > 32<<10 || json.Unmarshal(raw, &selected) != nil || !filepath.IsAbs(selected.Root) {
			return errors.New("existing desktop data location cannot be read")
		}
		*root = selected.Root
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := localnode.New(localnode.Options{Root: *root, Program: program})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if *inspect != "" {
		if !filepath.IsAbs(*inspect) {
			return errors.New("installer status output must be absolute")
		}
		preference, err := m.StartupPreference()
		if err != nil {
			return err
		}
		enabled := false
		if preference != nil {
			enabled = *preference
		} else if _, err := os.Stat(filepath.Join(*root, "device.json")); err == nil {
			status, err := m.Handle(ctx, localnode.Request{Operation: "startup.status"})
			if err != nil {
				return err
			}
			enabled = status.Startup != nil && status.Startup.RequestedEnabled
			if _, err := os.Stat(filepath.Join(*root, "startup.json")); errors.Is(err, os.ErrNotExist) {
				legacy, err := legacyStartup(ctx, *root)
				if err != nil {
					return err
				}
				enabled = legacy
			}
		}
		choice := "false"
		if enabled {
			choice = "true"
		}
		return os.WriteFile(*inspect, []byte("[MewDex]\r\nAutoStart="+choice+"\r\n"), 0600)
	}
	if *uninstall {
		*autostart = false
	}
	if *prepareUpdate {
		return m.CheckApplicationUpdate(ctx)
	}
	if !*uninstall {
		status, err := m.Handle(ctx, localnode.Request{Operation: "status"})
		if err != nil {
			return err
		}
		if status.Snapshot.Ready {
			// The normal update operation refuses active/unconfirmed work before
			// touching the current service. Keep the old tunnel until this succeeds.
			if _, err := m.Handle(ctx, localnode.Request{Operation: "service.update", ExpectedDeviceID: status.Snapshot.DeviceID, ExpectedInstanceID: status.Snapshot.InstanceID}); err != nil {
				return err
			}
		}
		legacy, err := legacyStartup(ctx, *root)
		if err != nil {
			return err
		}
		if legacy {
			status, err := m.Handle(ctx, localnode.Request{Operation: "status"})
			if err != nil {
				return err
			}
			if status.Snapshot.Configured && status.Snapshot.Hub {
				if _, err := m.Handle(ctx, localnode.Request{Operation: "proxy.import", ExpectedDeviceID: status.Snapshot.DeviceID}); err != nil {
					return err
				}
			}
		}
	}
	if err := m.SaveStartupPreference(ctx, *autostart); err != nil {
		return err
	}
	if *uninstall {
		if _, err := os.Stat(filepath.Join(*root, "device.json")); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		_, err = m.Handle(ctx, localnode.Request{Operation: "stop"})
		return err
	}
	if !filepath.IsAbs(*workbench) {
		return errors.New("installer requires an absolute workbench path")
	}
	return os.WriteFile(filepath.Join(*root, "workbench.json"), protocol.JSON(map[string]string{"path": *workbench}), 0600)
}

func legacyStartup(ctx context.Context, root string) (bool, error) {
	legacy := filepath.Join(root, "ingress")
	if _, err := os.Stat(filepath.Join(legacy, "hub-managed")); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(legacy, "startup.json")); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	_, binding, err := backhaul.Read(legacy)
	if err != nil {
		return false, err
	}
	backend, err := startup.Native()
	if err != nil {
		return false, err
	}
	controller, err := startup.New(legacy, backend)
	if err != nil {
		return false, err
	}
	status, err := controller.Status(ctx, binding)
	return status.RequestedEnabled, err
}
