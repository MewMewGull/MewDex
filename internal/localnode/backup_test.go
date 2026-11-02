package localnode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mewdex/internal/backup"
	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestDeviceBackupRetainsReceiptsAndHistoryWithoutImportingModelCredentials(t *testing.T) {
	m := testManager(t)
	fixtureEngine(t, m)
	ctx := context.Background()
	result, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true, Executor: true, Projects: []protocol.Project{{Path: t.TempDir()}}}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	session := protocol.Session{ID: "original-session", DeviceID: c.DeviceID, ProjectID: c.Projects[0].ID, State: "unknown", Frozen: true, EngineID: "original-engine-thread"}
	if err = s.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	command := protocol.Command{ID: "original-command", DeviceID: c.DeviceID, SessionID: session.ID, Kind: "start", SourceID: "original-input"}
	if _, _, err = s.Claim(command); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(protocol.CommandResult{CommandID: command.ID, State: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	historyDir := filepath.Join(c.Engine.Home, "sessions", "2026", "10", "28")
	if err = os.MkdirAll(historyDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(historyDir, "rollout-original.jsonl"), []byte("original engine history"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(historyDir, "provider-secret"), []byte("not a conversation"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Engine.Home, "auth.json"), []byte("model credential stays on its executor"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.DataDir, "control.json"), []byte("stale local control credential"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "device.mewdex-backup")
	backed, err := m.Handle(ctx, Request{Operation: "backup", ExpectedDeviceID: result.Snapshot.DeviceID, Backup: &BackupRequest{Path: archive, Passphrase: "test device backup passphrase"}})
	if err != nil {
		t.Fatal(err)
	}
	if backed.Backup == nil || backed.Snapshot.State != "stopped" || backed.Backup.DeviceID != c.DeviceID || !backed.Backup.EngineHistoryIncluded {
		t.Fatal("backup changed readiness or lost its identity")
	}
	parent := t.TempDir()
	manifest, err := backup.Extract(ctx, archive, "test device backup passphrase", parent, "staged", backup.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range manifest.Files {
		if strings.Contains(file.Name, "auth.json") || strings.Contains(file.Name, "provider-secret") || strings.Contains(file.Name, "control.json") || strings.Contains(file.Name, "service.lock") {
			t.Fatalf("non-backup state entered the archive: %s", file.Name)
		}
	}
	var profile backupProfile
	if err = json.Unmarshal(manifest.Metadata, &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Config.Engine.Executable != "" || len(profile.Config.Engine.Args) != 0 || profile.Config.DataDir != "data" {
		t.Fatal("backup metadata contained a runnable executable or host state path")
	}
	restored, err := store.Open(filepath.Join(parent, "staged", "data", "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	receipt, err := restored.Result(command.ID)
	if err != nil || receipt.State != "unknown" {
		t.Fatal("restored command lost its original unknown outcome")
	}
	var actual protocol.Session
	if err = restored.Get("sessions", session.ID, &actual); err != nil || actual.EngineID != session.EngineID || !actual.Frozen {
		t.Fatal("restored session lost its identity or frozen state")
	}
	data, err := os.ReadFile(filepath.Join(parent, "staged", "engine-home", "sessions", "2026", "10", "28", "rollout-original.jsonl"))
	if err != nil || string(data) != "original engine history" {
		t.Fatal("engine history was not retained")
	}
	after, err := os.ReadFile(m.profile())
	if err != nil || string(before) != string(after) {
		t.Fatal("backup modified the active device profile")
	}
}

func TestDeviceBackupRequiresStoppedServiceAndPreservesManualSettings(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	if _, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true}}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Operation: "backup", Backup: &BackupRequest{Path: filepath.Join(t.TempDir(), "device.age"), Passphrase: "test device backup passphrase"}}
	if _, err = m.Handle(ctx, request); err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatal("live state was backed up without its data lock")
	}
	lock.Close()
	c.Engine.Args = []string{"--custom-engine-parameter"}
	if err = replaceJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(ctx, request); err == nil {
		t.Fatal("manual settings were silently discarded")
	}
	if _, err = os.Stat(request.Backup.Path); !os.IsNotExist(err) {
		t.Fatal("rejected backup created an archive")
	}
	actual, err := config.Read(m.profile())
	if err != nil || len(actual.Engine.Args) != 1 {
		t.Fatal("rejected backup rewrote manual settings")
	}
}
