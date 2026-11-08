package localnode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestApplicationUpdatePreflightChecksStoppedTasksWithoutChangingConfiguration(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	if err := m.CheckApplicationUpdate(ctx); err != nil {
		t.Fatal("fresh installation preflight failed", err)
	}
	if _, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	session := protocol.Session{ID: "update-preflight", Title: "当前任务", State: "running"}
	if err := database.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckApplicationUpdate(ctx); err == nil {
		t.Fatal("installer preflight accepted an active task")
	}
	session.State = "idle"
	if err := database.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckApplicationUpdate(ctx); err != nil {
		t.Fatal("idle installation preflight failed", err)
	}
	after, _ := os.ReadFile(m.profile())
	if !bytes.Equal(before, after) {
		t.Fatal("installer preflight changed the device configuration")
	}
	status, err := m.Handle(ctx, Request{Operation: "status"})
	if err != nil || status.Snapshot.State != "stopped" {
		t.Fatal("installer preflight started a service", status.Snapshot.State, err)
	}
}

func TestServiceUpdateWaitsForComputerControlToFinish(t *testing.T) {
	c := config.Config{Executor: true, DataDir: t.TempDir()}
	database, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	value := protocol.ComputerSession{ID: "control", DeviceID: "node", ProjectID: "root", OwnerID: "conversation", InstanceID: "instance", State: "pending", CreatedAt: protocol.Now()}
	value, err = database.SaveComputerSession(value, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = updateIdle(c); err == nil {
		t.Fatal("service update ignored a pending local computer request")
	}
	value.State = "stopped"
	value.LastActionState = "unknown"
	if _, err = database.SaveComputerSession(value, value.Revision); err != nil {
		t.Fatal(err)
	}
	if err = updateIdle(c); err != nil {
		t.Fatal("stopped window still blocked the service update", err)
	}
	database.Close()
}

func TestServiceUpdateWaitsForAffectedExecutions(t *testing.T) {
	for _, item := range []struct {
		name, bucket, state, stop string
		retired, blocked          bool
	}{
		{"local running", "executor.db", "running", "", false, true},
		{"local unconfirmed", "executor.db", "unknown", "unconfirmed", false, true},
		{"local retired", "executor.db", "unknown", "unconfirmed", true, false},
		{"remote running through hub", "hub.db", "running", "", false, true},
		{"remote background through hub", "hub.db", "idle", "background-running", false, true},
		{"remote old outcome", "hub.db", "unknown", "unconfirmed", false, false},
		{"idle", "executor.db", "idle", "", false, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			c := config.Config{DeviceID: "own-device", DataDir: t.TempDir(), Executor: true, Hub: true}
			database, err := store.Open(filepath.Join(c.DataDir, item.bucket))
			if err != nil {
				t.Fatal(err)
			}
			session := protocol.Session{ID: "s", Title: "当前任务", DeviceID: "own-device", State: item.state, StopStatus: item.stop}
			if item.retired {
				session.ExecutionRecovery = &protocol.ExecutionRecovery{State: "retired"}
			}
			if err = database.Put("sessions", session.ID, session); err != nil {
				t.Fatal(err)
			}
			database.Close()
			if err = updateIdle(c); (err != nil) != item.blocked {
				t.Fatalf("blocked=%v, err=%v", item.blocked, err)
			}
		})
	}
}
