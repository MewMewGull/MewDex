package localnode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mewdex/internal/backup"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

const restorePassword = "test restore review passphrase"

func TestBackupHistoryVersionCompatibilityIsExplicit(t *testing.T) {
	for _, version := range []string{engine.CompatibleVersion, "0.154.0", "0.153.0", "0.162.2", "", "latest"} {
		profile := backupProfile{Format: 1, ProtocolVersion: protocol.Version, EngineVersion: version, Config: config.Config{DeviceID: "device", Name: "backup", DataDir: "data", Hub: true}}
		_, err := validateBackupProfile(backup.Manifest{Metadata: protocol.JSON(profile)})
		if (err == nil) != (version == engine.CompatibleVersion || version == "0.154.0") {
			t.Fatalf("unexpected history compatibility for %q: %v", version, err)
		}
	}
}

func TestPreparedExecutorRestoreCannotAdoptAnOlderRuntime(t *testing.T) {
	m, archive, _ := hubBackup(t)
	plan := prepareTestRestore(t, m, archive)
	record, err := m.loadRestore(&RestoreRequest{ID: plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	record.Info.Executor, record.EngineVersion = true, "0.154.0"
	if err = os.WriteFile(m.restoreRecord(plan.ID), protocol.JSON(record), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(context.Background(), Request{Operation: "restore.apply", Restore: &RestoreRequest{ID: plan.ID}}); err == nil || !strings.Contains(err.Error(), "another engine version") {
		t.Fatal("accepted stale runtime plan", err)
	}
	after, err := os.ReadFile(m.profile())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("stale runtime changed active profile", err)
	}
	if _, err = m.Handle(context.Background(), Request{Operation: "restore.prepare", Restore: &RestoreRequest{ID: plan.ID, Path: archive, Passphrase: restorePassword}}); err == nil || !strings.Contains(err.Error(), "another engine version") {
		t.Fatal("reused stale runtime plan", err)
	}
}

func hubBackup(t *testing.T) (*Manager, string, config.Config) {
	t.Helper()
	m := testManager(t)
	if _, err := m.Handle(context.Background(), Request{Operation: "setup", Setup: &Setup{Hub: true, Name: "restore test"}}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put("test", "original", map[string]string{"value": "snapshot"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	archive := filepath.Join(t.TempDir(), "state.age")
	if _, err = m.Handle(context.Background(), Request{Operation: "backup", Backup: &BackupRequest{Path: archive, Passphrase: restorePassword}}); err != nil {
		t.Fatal(err)
	}
	return m, archive, c
}

func prepareTestRestore(t *testing.T, m *Manager, archive string) *RestoreInfo {
	t.Helper()
	result, err := m.Handle(context.Background(), Request{Operation: "restore.prepare", Restore: &RestoreRequest{Path: archive, Passphrase: restorePassword}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Restore == nil || result.Restore.Applied {
		t.Fatal("prepare adopted the restore")
	}
	return result.Restore
}

func TestRestoreSwitchIsExplicitAndPreservesThePreviousProfileAndData(t *testing.T) {
	m := testManager(t)
	fixtureEngine(t, m)
	ctx := context.Background()
	if _, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true, Executor: true, Projects: []protocol.Project{{Path: t.TempDir()}}}}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	hs, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	code, err := hs.Invite("admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := hs.Pair(code, "old input", "")
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := hs.Invite("client", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	hs.Close()
	xs, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = xs.Put("sessions", "original", protocol.Session{ID: "original", DeviceID: c.DeviceID, ProjectID: c.Projects[0].ID, State: "running", EngineID: "engine-thread", TurnID: "old-turn", EngineStarted: true}); err != nil {
		t.Fatal(err)
	}
	if err = xs.Put("project_bindings", c.Projects[0].ID, map[string]string{"id": c.Projects[0].ID, "deviceId": c.DeviceID, "path": c.Projects[0].Path}); err != nil {
		t.Fatal(err)
	}
	xs.Close()
	history := filepath.Join(c.Engine.Home, "sessions", "2026")
	if err = os.MkdirAll(history, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(history, "rollout-test.jsonl"), []byte("retained history"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Engine.Home, "auth.json"), []byte("model secret stays in its original directory"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "state.age")
	if _, err = m.Handle(ctx, Request{Operation: "backup", Backup: &BackupRequest{Path: archive, Passphrase: restorePassword}}); err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	oldDB, err := os.ReadFile(filepath.Join(c.DataDir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	plan := prepareTestRestore(t, m, archive)
	current, _ := os.ReadFile(m.profile())
	if !bytes.Equal(previous, current) {
		t.Fatal("preparation changed the live profile")
	}
	if !plan.RequiresPairing || !plan.RequiresModelLogin || !plan.PreviousStatePreserved {
		t.Fatal("restore review omitted its access or account consequences")
	}
	result, err := m.Handle(ctx, Request{Operation: "restore.apply", ExpectedDeviceID: c.DeviceID, Restore: &RestoreRequest{ID: plan.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Restore == nil || !result.Restore.Applied || result.Snapshot.State != "stopped" {
		t.Fatal("restore was not explicitly adopted while stopped")
	}
	adopted, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	if adopted.DataDir == c.DataDir || adopted.Engine.Executable == "" || adopted.Engine.Home == c.Engine.Home {
		t.Fatal("restore did not select independent state and the trusted engine")
	}
	retained, err := os.ReadFile(filepath.Join(m.options.Root, "restore-plans", plan.ID+".previous.json"))
	if err != nil || !bytes.Equal(retained, previous) {
		t.Fatal("previous profile was not preserved exactly")
	}
	stillOld, err := os.ReadFile(filepath.Join(c.DataDir, "hub.db"))
	if err != nil || !bytes.Equal(stillOld, oldDB) {
		t.Fatal("adoption modified the previous database")
	}
	if _, err = os.Stat(filepath.Join(adopted.Engine.Home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("model credential was imported")
	}
	hs, err = store.Open(filepath.Join(adopted.DataDir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hs.Authenticate(token); err == nil {
		t.Fatal("snapshot credential became active")
	}
	if _, _, err = hs.Pair(invitation, "input", ""); err == nil {
		t.Fatal("snapshot invitation became active")
	}
	hs.Close()
	xs, err = store.Open(filepath.Join(adopted.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	var session protocol.Session
	if err = xs.Get("sessions", "original", &session); err != nil || session.State != "unknown" || !session.Frozen || session.TurnID != "old-turn" {
		t.Fatal("restored task could execute without confirmation")
	}
	xs.Close()
	again, err := m.Handle(ctx, Request{Operation: "restore.apply", Restore: &RestoreRequest{ID: plan.ID}})
	if err != nil || !again.Restore.Applied {
		t.Fatal("lost apply response could not be observed idempotently")
	}
	status, err := m.Handle(ctx, Request{Operation: "restore.status", Restore: &RestoreRequest{ID: plan.ID}})
	if err != nil || !status.Restore.Applied {
		t.Fatal("restore status did not report the committed profile")
	}
}

func TestRestoreRejectsChangesMadeAfterReview(t *testing.T) {
	for _, kind := range []string{"profile", "data", "prepared"} {
		t.Run(kind, func(t *testing.T) {
			m, archive, c := hubBackup(t)
			plan := prepareTestRestore(t, m, archive)
			switch kind {
			case "profile":
				c.Name = "edited after review"
				if err := replaceJSON(m.profile(), c); err != nil {
					t.Fatal(err)
				}
			case "data":
				s, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Put("test", "new-work", map[string]string{"value": "later"}); err != nil {
					t.Fatal(err)
				}
				s.Close()
			case "prepared":
				if err := os.WriteFile(filepath.Join(m.restoreDirectory(plan.ID), "injected"), []byte("undeclared"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(m.profile())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.Handle(context.Background(), Request{Operation: "restore.apply", Restore: &RestoreRequest{ID: plan.ID}}); err == nil {
				t.Fatal("changed review target was adopted")
			}
			after, err := os.ReadFile(m.profile())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed restore replaced the current profile")
			}
		})
	}
}

func TestFreshAndUnreadableInstallationsCanPrepareAndAdoptABackup(t *testing.T) {
	_, archive, _ := hubBackup(t)
	for _, broken := range []bool{false, true} {
		target := testManager(t)
		if broken {
			if err := createFile(target.profile(), []byte("{broken profile")); err != nil {
				t.Fatal(err)
			}
		}
		plan := prepareTestRestore(t, target, archive)
		if _, err := target.Handle(context.Background(), Request{Operation: "restore.apply", Restore: &RestoreRequest{ID: plan.ID}}); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Read(target.profile()); err != nil {
			t.Fatal("restored profile is not readable")
		}
	}
}

func TestInstallationLeasePreventsConfigurationFromHidingARunningInstance(t *testing.T) {
	m, _, c := hubBackup(t)
	lease, err := lifecycle.Installation(m.options.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	c.DataDir = filepath.Join(m.options.Root, "another-data")
	if err = replaceJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	status, err := m.Handle(context.Background(), Request{Operation: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Snapshot.State != "unknown" || status.Snapshot.Ready {
		t.Fatal("changed data path disguised a running installation as stopped")
	}
	if _, err = m.Handle(context.Background(), Request{Operation: "projects", Projects: c.Projects}); err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatal("installation lease did not block state changes")
	}
}

func TestPreparationReceiptIsRecoverableByItsCallerID(t *testing.T) {
	m, archive, _ := hubBackup(t)
	id := protocol.ID()
	request := Request{Operation: "restore.prepare", Restore: &RestoreRequest{ID: id, Path: archive, Passphrase: restorePassword}}
	first, err := m.Handle(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Handle(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Restore.ID != id || second.Restore.ID != id {
		t.Fatal("lost preparation receipt created another restore")
	}
	entries, err := os.ReadDir(filepath.Join(m.options.Root, "restores"))
	if err != nil || len(entries) != 1 {
		t.Fatal("preparation was duplicated")
	}
	changed := filepath.Join(t.TempDir(), "other.age")
	if err = os.WriteFile(changed, []byte("a different archive"), 0600); err != nil {
		t.Fatal(err)
	}
	request.Restore.Path = changed
	if _, err = m.Handle(context.Background(), request); err == nil {
		t.Fatal("restore ID was reused for different contents")
	}
}
