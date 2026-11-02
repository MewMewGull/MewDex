package localnode

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/runtimefiles"
)

func (m *Manager) start(ctx context.Context) error {
	c, err := config.Load(m.profile())
	if err != nil {
		return err
	}
	if m.describe(c).PairingRequired {
		return errors.New("pair this executor with its Hub before starting")
	}
	state, err := m.status(ctx)
	if err != nil {
		return err
	}
	if state.Ready {
		return nil
	}
	if state.State == "unknown" || state.State == "stopping" {
		return errors.New("local service state must be confirmed before another start")
	}
	if !c.Hub && c.Executor && state.State == "stopped" {
		if err = m.pairExecutor(ctx, c, ""); err != nil {
			return err
		}
	}
	var exited <-chan error
	if state.State != "starting" {
		registered, err := m.startRegistered(ctx)
		if err != nil {
			return err
		}
		if !registered {
			installation, err := lifecycle.Installation(m.options.Root)
			if err != nil {
				return errors.New("local installation still owns its service lease")
			}
			installation.Close()
			probe, err := lifecycle.Acquire(c.DataDir)
			if err != nil {
				return errors.New("local service still owns its data directory")
			}
			probe.Close()
			log, err := OpenServiceLog(m.options.Root)
			if err != nil {
				return err
			}
			defer log.Close()
			program, err := runtimefiles.Program(m.options.Root, m.options.Program)
			if err != nil {
				return err
			}
			command := exec.Command(program, "serve", "-config", m.profile(), "-install-root", m.options.Root)
			command.Dir = m.options.Root
			command.Stdout, command.Stderr = log, log
			detach(command)
			if err = command.Start(); err != nil {
				return errors.New("local service process could not be started")
			}
			done := make(chan error, 1)
			exited = done
			go func() { done <- command.Wait() }()
		}
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err = m.status(ctx)
		if err == nil && state.Ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("service startup is still unconfirmed; query its status before retrying")
		case <-exited:
			return errors.New("service exited during startup; inspect its local service log")
		case <-ticker.C:
		}
	}
}

func (m *Manager) stop(ctx context.Context, instance string) error {
	c, err := config.Read(m.profile())
	if err != nil {
		return err
	}
	state, err := m.status(ctx)
	if err != nil {
		return err
	}
	if state.State == "stopped" {
		return nil
	}
	if state.State == "unknown" {
		return errors.New("local control channel is unavailable; no process was killed")
	}
	if instance != "" && instance != state.InstanceID {
		return errors.New("local service instance changed; refresh before stopping")
	}
	record, err := lifecycle.Read(c.DataDir)
	if err != nil {
		return err
	}
	if record.DeviceID != c.DeviceID || record.InstanceID != state.InstanceID {
		return errors.New("local service instance changed; no stop was sent")
	}
	if _, err = lifecycle.Call(ctx, record, true); err != nil {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		probe, err := lifecycle.Acquire(c.DataDir)
		if err == nil {
			probe.Close()
			installation, leaseErr := lifecycle.Installation(m.options.Root)
			if leaseErr == nil {
				installation.Close()
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("service is stopping; query its status before another operation")
		case <-ticker.C:
		}
	}
}
