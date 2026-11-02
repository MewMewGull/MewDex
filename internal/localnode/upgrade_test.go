package localnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"mewdex/internal/runtimefiles"
	"mewdex/internal/store"
)

func upgradeFixture(t *testing.T) (*Manager, config.Config, string) {
	t.Helper()
	m := testManager(t)
	fixtureEngine(t, m)
	if _, err := m.Handle(context.Background(), Request{Operation: "setup", Setup: &Setup{Hub: true, Executor: true, Projects: []protocol.Project{{Path: t.TempDir()}}}}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	// Only this test's staged legacy resource changes. The current app's
	// bundled source remains the new pinned version.
	manifestPath := filepath.Join(filepath.Dir(filepath.Dir(c.Engine.Executable)), "mewdex-engine.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest runtimefiles.Manifest
	if json.Unmarshal(raw, &manifest) != nil {
		t.Fatal("invalid fixture manifest")
	}
	manifest.Version = "0.154.0"
	legacySource := t.TempDir()
	if err = os.Mkdir(filepath.Join(legacySource, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(m.options.Engine)
	if err != nil {
		t.Fatal(err)
	}
	legacyProgram := filepath.Join(legacySource, "bin", "codex.exe")
	if err = os.WriteFile(legacyProgram, program, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(legacySource, "mewdex-engine.json"), protocol.JSON(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	c.Engine.Executable, err = runtimefiles.Engine(m.options.Root, legacyProgram, "0.154.0")
	if err != nil {
		t.Fatal(err)
	}
	if err = replaceJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(c.Engine.Home, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Engine.Home, "auth.json"), []byte("owned synthetic model account marker"), 0600); err != nil {
		t.Fatal(err)
	}
	hs, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := hs.Invite("admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := hs.Pair(invitation, "existing input", "")
	if err != nil {
		t.Fatal(err)
	}
	hs.Close()
	xs, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer xs.Close()
	session := protocol.Session{ID: "session", DeviceID: c.DeviceID, ProjectID: c.Projects[0].ID, EngineID: "old-thread", EngineStarted: true, ExecutionScopeID: "old-scope", State: "running", TurnID: "original-turn"}
	if err = xs.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	if err = xs.Put("project_bindings", c.Projects[0].ID, map[string]string{"id": c.Projects[0].ID, "deviceId": c.DeviceID, "path": c.Projects[0].Path}); err != nil {
		t.Fatal(err)
	}
	if err = xs.Put("queue_items", "waiting", protocol.QueueItem{ID: "waiting", DeviceID: c.DeviceID, SessionID: session.ID, State: "queued", Input: []protocol.Input{{Type: "text", Text: "do not send"}}}); err != nil {
		t.Fatal(err)
	}
	return m, c, token
}

func TestUpgradePreservesOriginalStateAndCurrentAuthorization(t *testing.T) {
	m, c, token := upgradeFixture(t)
	profile, _ := os.ReadFile(m.profile())
	database, _ := os.ReadFile(filepath.Join(c.DataDir, "executor.db"))
	id := protocol.ID()
	prepared, err := m.Handle(context.Background(), Request{Operation: "upgrade.prepare", Upgrade: &UpgradeRequest{ID: id}})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Upgrade == nil || prepared.Upgrade.Applied || prepared.Upgrade.SourceEngineVersion != "0.154.0" || prepared.Upgrade.TargetEngineVersion != engine.CompatibleVersion {
		t.Fatal("upgrade did not return an independent reviewed plan")
	}
	stillProfile, _ := os.ReadFile(m.profile())
	if !bytes.Equal(stillProfile, profile) {
		t.Fatal("preparation changed the active profile")
	}
	staged, err := config.Read(filepath.Join(m.upgradeDirectory(id), "device.json"))
	if err != nil {
		t.Fatal(err)
	}
	if staged.DataDir == c.DataDir || staged.Engine.Home != c.Engine.Home || staged.DeviceID != c.DeviceID {
		t.Fatal("upgrade replaced model account or device identity")
	}
	xs, err := store.Open(filepath.Join(staged.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	var session protocol.Session
	if err = xs.Get("sessions", "session", &session); err != nil {
		t.Fatal(err)
	}
	xs.Close()
	if !session.Frozen || session.State != "unknown" || session.ExecutionScopeID != "" || session.EngineID != "old-thread" || session.TurnID != "original-turn" {
		t.Fatal("migration replayed or rebound an unconfirmed task")
	}
	hs, err := store.Open(filepath.Join(staged.DataDir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = hs.Authenticate(token); err != nil {
		t.Fatal("upgrade revoked the existing input", err)
	}
	hs.Close()
	if _, err = os.Stat(filepath.Join(m.upgradeDirectory(id), "engine-home")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("upgrade copied model account files", err)
	}
	result, err := m.Handle(context.Background(), Request{Operation: "upgrade.apply", Upgrade: &UpgradeRequest{ID: id}})
	if err != nil || result.Upgrade == nil || !result.Upgrade.Applied {
		t.Fatal("upgrade could not be adopted", err)
	}
	adopted, err := config.Read(m.profile())
	if err != nil || adopted.DataDir != staged.DataDir || adopted.Engine.Home != c.Engine.Home {
		t.Fatal("upgrade adopted the wrong configuration", err)
	}
	retained, _ := os.ReadFile(filepath.Join(m.options.Root, "upgrade-plans", id+".previous.json"))
	originalDB, _ := os.ReadFile(filepath.Join(c.DataDir, "executor.db"))
	if !bytes.Equal(retained, profile) || !bytes.Equal(originalDB, database) {
		t.Fatal("upgrade changed the previous state")
	}
	for _, operation := range []string{"upgrade.apply", "upgrade.prepare", "upgrade.status"} {
		result, err = m.Handle(context.Background(), Request{Operation: operation, Upgrade: &UpgradeRequest{ID: id}})
		if err != nil || result.Upgrade == nil || !result.Upgrade.Applied {
			t.Fatal("lost response could not recover the same completed upgrade", operation, err)
		}
	}
}

func TestUpgradeRejectsReviewDriftAndLeavesCurrentProfile(t *testing.T) {
	for _, kind := range []string{"data", "prepared", "program", "credentials", "engine"} {
		t.Run(kind, func(t *testing.T) {
			m, c, _ := upgradeFixture(t)
			if kind == "credentials" {
				c.NodeTokenFile = filepath.Join(m.options.Root, "node-token")
				if err := os.WriteFile(c.NodeTokenFile, []byte("old synthetic token"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := replaceJSON(m.profile(), c); err != nil {
					t.Fatal(err)
				}
			}
			id := protocol.ID()
			if _, err := m.Handle(context.Background(), Request{Operation: "upgrade.prepare", Upgrade: &UpgradeRequest{ID: id}}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(m.profile())
			switch kind {
			case "data":
				s, err := store.Open(filepath.Join(c.DataDir, "hub.db"))
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Put("changed", "value", true); err != nil {
					t.Fatal(err)
				}
				s.Close()
			case "prepared":
				if err := os.WriteFile(filepath.Join(m.upgradeDirectory(id), "changed.txt"), []byte("host edit"), 0600); err != nil {
					t.Fatal(err)
				}
			case "program":
				m.options.Program = filepath.Join(t.TempDir(), "other-program")
				if err := os.WriteFile(m.options.Program, []byte("new application"), 0700); err != nil {
					t.Fatal(err)
				}
			case "credentials":
				if err := os.WriteFile(c.NodeTokenFile, []byte("new synthetic token"), 0600); err != nil {
					t.Fatal(err)
				}
			case "engine":
				staged, err := config.Read(filepath.Join(m.upgradeDirectory(id), "device.json"))
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(filepath.Dir(filepath.Dir(staged.Engine.Executable)), "mewdex-engine.json")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, append(raw, ' '), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.Handle(context.Background(), Request{Operation: "upgrade.apply", Upgrade: &UpgradeRequest{ID: id}}); err == nil {
				t.Fatal("adopted changed " + kind)
			}
			after, _ := os.ReadFile(m.profile())
			if !bytes.Equal(before, after) {
				t.Fatal("failed upgrade changed the active profile")
			}
		})
	}
}

func TestUpgradeRequiresStoppedServiceAndCleansCancelledStage(t *testing.T) {
	m, c, _ := upgradeFixture(t)
	lease, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(context.Background(), Request{Operation: "upgrade.prepare", Upgrade: &UpgradeRequest{ID: protocol.ID()}}); err == nil {
		t.Fatal("copied a running service")
	}
	lease.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.options.Report = func(value OperationProgress) {
		if value.Phase == "copying" {
			cancel()
		}
	}
	id := protocol.ID()
	before, _ := os.ReadFile(m.profile())
	if _, err = m.Handle(ctx, Request{Operation: "upgrade.prepare", Upgrade: &UpgradeRequest{ID: id}}); !errors.Is(err, context.Canceled) {
		t.Fatal("upgrade ignored copy cancellation", err)
	}
	if _, err = os.Stat(m.upgradeDirectory(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled upgrade retained its partial copy", err)
	}
	after, _ := os.ReadFile(m.profile())
	if !bytes.Equal(before, after) {
		t.Fatal("cancelled upgrade changed its source profile")
	}
}

func TestUpgradeJobRecoversPreparedAndAppliedResults(t *testing.T) {
	m, _, _ := upgradeFixture(t)
	prepare := JobRequest{ID: protocol.ID(), Operation: "upgrade.prepare"}
	prepare.Upgrade = &UpgradeRequest{ID: prepare.ID}
	seedJob(t, m, prepare, protocol.Now())
	if err := m.RunJob(context.Background(), prepare); err != nil {
		t.Fatal(err)
	}
	plan := readJob(t, m, prepare.ID).Upgrade
	if plan == nil || plan.Applied {
		t.Fatal("upgrade job skipped review")
	}
	record, _ := m.loadJob(prepare.ID)
	record.Info.State, record.Info.Upgrade = "running", nil
	if err := m.saveJob(record); err != nil {
		t.Fatal(err)
	}
	if recovered := readJob(t, m, prepare.ID); recovered.State != "succeeded" || recovered.Upgrade == nil {
		t.Fatal("prepared upgrade was not recovered after worker loss")
	}
	apply := JobRequest{ID: protocol.ID(), Operation: "upgrade.apply", Upgrade: &UpgradeRequest{ID: plan.ID}}
	seedJob(t, m, apply, protocol.Now())
	if err := m.RunJob(context.Background(), apply); err != nil {
		t.Fatal(err)
	}
	job := readJob(t, m, apply.ID)
	if job.State != "succeeded" || job.Upgrade == nil || !job.Upgrade.Applied {
		t.Fatal("upgrade job did not commit")
	}
	value, _ := os.ReadFile(m.jobPath(apply.ID))
	if strings.Contains(string(value), "synthetic model account marker") {
		t.Fatal("model credentials entered operation records")
	}
}

func TestLegacyInstalledHistoryIsExportedWithItsActualVersionAndPreparedForCurrentRuntime(t *testing.T) {
	m, c, _ := upgradeFixture(t)
	archive := filepath.Join(t.TempDir(), "legacy.age")
	if _, err := m.Handle(context.Background(), Request{Operation: "backup", Backup: &BackupRequest{Path: archive, Passphrase: restorePassword}}); err != nil {
		t.Fatal(err)
	}
	manifest, err := backup.Extract(context.Background(), archive, restorePassword, t.TempDir(), "inspected", backup.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := validateBackupProfile(manifest)
	if err != nil || profile.EngineVersion != "0.154.0" {
		t.Fatal("export mislabeled old engine history", err)
	}
	plan := prepareTestRestore(t, m, archive)
	staged, err := config.Read(filepath.Join(m.restoreDirectory(plan.ID), "device.json"))
	if err != nil {
		t.Fatal(err)
	}
	version, err := runtimefiles.EngineVersion(staged.Engine.Executable)
	if err != nil || version != engine.CompatibleVersion || staged.Engine.Home == c.Engine.Home {
		t.Fatal("legacy backup did not prepare isolated current runtime", err)
	}
}
