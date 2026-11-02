package backhaul

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/serviceguard"
	"mewdex/internal/startup"
)

func Run(ctx context.Context, root string, c Config, binding startup.Binding) error {
	return run(ctx, root, c, binding, nil)
}

func RunManaged(ctx context.Context, root string, c Config, binding startup.Binding, controller *startup.Controller, owner, program string) error {
	return run(ctx, root, c, binding, func() (bool, error) { return controller.Authorized(owner, program, binding) })
}

func run(ctx context.Context, root string, c Config, binding startup.Binding, authorize func() (bool, error)) error {
	installation, err := lifecycle.Installation(root)
	if err != nil {
		return err
	}
	defer installation.Close()
	if authorize != nil {
		allowed, err := authorize()
		if err != nil || !allowed {
			return err
		}
	}
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0700); err != nil {
		return err
	}
	return serviceguard.Run(ctx, data, installation, true, func(ctx context.Context) error {
		return runLoop(ctx, root, data, c, binding)
	})
}

func runLoop(ctx context.Context, root, data string, c Config, binding startup.Binding) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	control, err := lifecycle.Start(data, c.ID, cancel)
	if err != nil {
		return err
	}
	defer control.Close()
	r := newSupervisor(root, c, binding, func(state State) error { return saveState(data, state) })
	r.state.InstanceID = control.InstanceID()
	if err := r.publish("starting", "", 0); err != nil {
		return err
	}
	control.Ready("") // Worker readiness and public ingress health are separate.
	return r.run(ctx)
}

func saveState(data string, state State) error {
	root, err := os.OpenRoot(data)
	if err != nil {
		return err
	}
	defer root.Close()
	name := "state." + protocol.ID() + ".tmp"
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	_, writeErr := file.Write(protocol.JSON(state))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	return root.Rename(name, "state.json")
}

func Status(ctx context.Context, root string) (map[string]any, error) {
	data := filepath.Join(root, "data")
	result := map[string]any{"workerReachable": false, "connected": false}
	var state State
	raw, err := os.ReadFile(filepath.Join(data, "state.json"))
	if err == nil && len(raw) <= 32<<10 && json.Unmarshal(raw, &state) == nil {
		result["lastRecorded"] = state
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	record, err := lifecycle.Read(data)
	if err != nil {
		return result, nil
	}
	worker, err := lifecycle.Call(ctx, record, false)
	if err != nil {
		result["workerState"] = "unreachable"
		return result, nil
	}
	result["worker"], result["workerReachable"] = worker, true
	_, binding, err := Read(root)
	result["matchesProfile"] = err == nil && state.ProfileHash == binding.ProfileSHA256
	updated, timeErr := time.Parse(time.RFC3339Nano, state.UpdatedAt)
	fresh := timeErr == nil && time.Since(updated) >= -30*time.Second && time.Since(updated) < 30*time.Second
	result["connected"] = err == nil && state.ProfileHash == binding.ProfileSHA256 && state.InstanceID == record.InstanceID && state.ID == record.DeviceID && fresh && state.State == "connected"
	return result, nil
}
