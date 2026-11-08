package localnode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/serviceguard"
	"mewdex/internal/serviceport"
	"mewdex/internal/startup"
)

func (m *Manager) startupController() (*startup.Controller, error) {
	backend := m.options.StartupBackend
	if backend == nil {
		var err error
		backend, err = startup.Native()
		if err != nil {
			return nil, err
		}
	}
	return startup.New(m.options.Root, backend)
}

func (m *Manager) startupBinding() (startup.Binding, error) {
	info, err := os.Lstat(m.profile())
	if err != nil {
		return startup.Binding{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return startup.Binding{}, errors.New("startup requires a bounded regular device profile")
	}
	file, err := os.Open(m.profile())
	if err != nil {
		return startup.Binding{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return startup.Binding{}, errors.New("device profile exceeds the startup binding limit")
	}
	var identity struct {
		DeviceID string `json:"deviceId"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.DeviceID == "" {
		return startup.Binding{}, errors.New("startup requires a valid device identity")
	}
	return startup.Binding{DeviceID: identity.DeviceID, ProfileSHA256: startup.Fingerprint(raw)}, nil
}

func (m *Manager) startupStatus(ctx context.Context) (StartupInfo, error) {
	controller, err := m.startupController()
	if err != nil {
		return StartupInfo{Supported: false, Message: err.Error()}, nil
	}
	binding, err := m.startupBinding()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return StartupInfo{}, err
	}
	info, err := controller.Status(ctx, binding)
	return StartupInfo(info), err
}

func (m *Manager) applyStartup(ctx context.Context, request *StartupRequest) (*StartupInfo, error) {
	if request == nil {
		return nil, errors.New("startup settings are required")
	}
	binding, err := m.startupBinding()
	if err != nil {
		return nil, err
	}
	controller, err := m.startupController()
	if err != nil {
		return nil, err
	}
	program := ""
	if request.Enabled {
		log, logErr := OpenServiceLog(m.options.Root)
		if logErr != nil {
			return nil, logErr
		}
		if logErr = log.Close(); logErr != nil {
			return nil, logErr
		}
		program, err = runtimefiles.Program(m.options.Root, m.options.Program)
		if err != nil {
			return nil, err
		}
	}
	info, err := controller.Apply(ctx, request.Enabled, program, binding)
	if err == nil {
		err = m.saveStartupPreference(request.Enabled)
	}
	value := StartupInfo(info)
	return &value, err
}

func (m *Manager) startRegistered(ctx context.Context) (bool, error) {
	if _, err := os.Lstat(filepath.Join(m.options.Root, "startup.json")); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	controller, err := m.startupController()
	if err != nil {
		return false, err
	}
	binding, err := m.startupBinding()
	if err != nil {
		return false, err
	}
	info, err := controller.Status(ctx, binding)
	if err != nil {
		return false, err
	}
	if !info.RequestedEnabled {
		return false, nil
	}
	if !info.Enabled || !info.MatchesProfile || !info.EntryMatches {
		return true, errors.New("review or disable the registered startup entry before starting this service")
	}
	return true, controller.Start(ctx, binding)
}

func (m *Manager) pauseStartupRecovery() error {
	if _, err := os.Lstat(filepath.Join(m.options.Root, "startup.json")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	controller, err := m.startupController()
	if err != nil {
		return err
	}
	return controller.PauseRecovery()
}

// RunManaged is the foreground entry used by the OS manager. The installation
// lease remains owned through serve, including engine cleanup. The existing
// service path acquires its data lock and control record as usual.
func (m *Manager) RunManaged(ctx context.Context, owner string, serve func(context.Context, config.Config) error) error {
	controller, err := m.startupController()
	if err != nil {
		return err
	}
	state, err := m.status(ctx)
	if err != nil {
		return err
	}
	if state.Ready || state.State == "unknown" || state.State == "starting" || state.State == "stopping" {
		return nil
	}
	lease, err := lifecycle.Installation(m.options.Root)
	if err != nil {
		return err
	} // Another operation owns this installation; do not race it.
	defer lease.Close()
	binding, err := m.startupBinding()
	if err != nil {
		return err
	}
	allowed, err := controller.Authorized(owner, m.options.Program, binding)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	} // Disabled, restored, changed or superseded: no automatic adoption.
	c, err := config.Load(m.profile())
	if err != nil {
		return err
	}
	if m.describe(c).PairingRequired {
		return nil
	}
	if c.Hub {
		if err := serviceport.Check(c.Listen); err != nil {
			var occupied *serviceport.OccupiedError
			if errors.As(err, &occupied) && discoverHub(ctx, c) != nil {
				return nil
			}
			return err
		}
	}
	if !c.Hub && c.Executor {
		if err = m.pairExecutor(ctx, c, ""); err != nil {
			return err
		}
	}
	c.InstallationRoot = "" // This function already owns that lease for the entire callback.
	err = serviceguard.Run(ctx, c.DataDir, lease, c.Executor || c.Hub, func(ctx context.Context) error { return serve(ctx, c) })
	if err == nil && ctx.Err() != nil {
		// Preserve OS-manager failure recovery for an interrupted managed
		// process. An authenticated local stop cancels only serve's child scope.
		return ctx.Err()
	}
	return err
}
