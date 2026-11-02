package localnode

import (
	"path/filepath"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

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
