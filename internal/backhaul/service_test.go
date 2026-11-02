package backhaul

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
)

func TestAReopenedWorkerCannotReuseAnOldConnectedState(t *testing.T) {
	root, c := fixtureConfig(t)
	writeProfile(t, root, c)
	_, binding, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	control, err := lifecycle.Start(data, c.ID, cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.Ready("")
	state := State{ID: c.ID, ProfileHash: binding.ProfileSHA256, InstanceID: "previous-worker", State: "connected", UpdatedAt: protocol.Now()}
	if err := saveState(data, state); err != nil {
		t.Fatal(err)
	}
	if result, err := Status(ctx, root); err != nil || result["workerReachable"] != true || result["connected"] != false {
		t.Fatal("earlier worker's health was adopted", result, err)
	}
	state.InstanceID = control.InstanceID()
	if err := saveState(data, state); err != nil {
		t.Fatal(err)
	}
	if result, err := Status(ctx, root); err != nil || result["connected"] != true {
		t.Fatal(result, err)
	}
	state.UpdatedAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	if err := saveState(data, state); err != nil {
		t.Fatal(err)
	}
	if result, err := Status(ctx, root); err != nil || result["connected"] != false {
		t.Fatal("stale health stayed connected", result, err)
	}
}

func TestStartupAuthorizationRunsUnderTheInstallationLease(t *testing.T) {
	root, c := fixtureConfig(t)
	writeProfile(t, root, c)
	_, binding, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = run(context.Background(), root, c, binding, func() (bool, error) {
		called = true
		if lease, err := lifecycle.Installation(root); err == nil {
			lease.Close()
			t.Fatal("startup decision raced an installation change")
		}
		return false, nil
	})
	if err != nil || !called {
		t.Fatal(err, called)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "control.json")); !os.IsNotExist(err) {
		t.Fatal("disabled registration started a worker", err)
	}
}
