package localnode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestSnapshotReadsExecutorConnectionWithoutCreatingADatabase(t *testing.T) {
	m, c := repairFixture(t, "https://hub.example", []byte("test-credential"))
	c.DataDir = filepath.Join(m.options.Root, "状态 # data")
	path := filepath.Join(c.DataDir, "executor.db")
	if snapshot := m.describe(c); snapshot.HubConnection != nil {
		t.Fatal("a missing connection record was reported as connected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("status inspection created a database", err)
	}
	database, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// Old connection records have only these fields and remain readable.
	if err = database.Put("status", "connection", map[string]string{"state": "disconnected", "at": "previous", "error": "offline"}); err != nil {
		t.Fatal(err)
	}
	if snapshot := m.describe(c); snapshot.HubConnection == nil || snapshot.HubConnection.State != "disconnected" {
		t.Fatalf("old live database status was unavailable: %+v", snapshot.HubConnection)
	}
	wanted := protocol.NodeConnection{State: "pairing-required", At: protocol.Now(), Error: "credential rejected", RetryAt: protocol.Now()}
	if err = database.Put("status", "connection", wanted); err != nil {
		t.Fatal(err)
	}
	if snapshot := m.describe(c); snapshot.HubConnection == nil || *snapshot.HubConnection != wanted {
		t.Fatalf("connection details did not update: %+v", snapshot.HubConnection)
	}
}

func TestStoppedSnapshotDoesNotReuseAnOldConnectedStatus(t *testing.T) {
	m, c := repairFixture(t, "https://hub.example", []byte("test-credential"))
	database, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Put("status", "connection", protocol.NodeConnection{State: "connected", At: "previous-connection"}); err != nil {
		t.Fatal(err)
	}
	database.Close()
	snapshot, err := m.status(context.Background())
	if err != nil || snapshot.State != "stopped" || snapshot.HubConnection == nil || snapshot.HubConnection.State != "disconnected" || snapshot.HubConnection.At != "" {
		t.Fatal("stopped service reported a live connection", snapshot, err)
	}
}
