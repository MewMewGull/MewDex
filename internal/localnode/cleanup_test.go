package localnode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

const cleanupBootA = "11111111-1111-4111-8111-111111111111"
const cleanupBootB = "22222222-2222-4222-8222-222222222222"

func cleanupFixture(t *testing.T) (*Manager, config.Config, func(string), map[string][]byte) {
	t.Helper()
	m := testManager(t)
	boot := cleanupBootA
	m.options.BootSession = func() (string, error) { return boot, nil }
	if _, err := m.Handle(context.Background(), Request{Operation: "setup", Setup: &Setup{Name: "frozen synthetic fixture", Hub: true, Listen: "127.0.0.1:0"}}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Read(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(m.options.Root, "installation"), c.DataDir} {
		// Legacy format exercises the required pre-restart checkpoint path.
		if err := os.WriteFile(filepath.Join(dir, lifecycle.OwnershipPending), []byte("old-unconfirmed-fixture-lifetime"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	protected := map[string][]byte{}
	profile, err := os.ReadFile(m.profile())
	if err != nil {
		t.Fatal(err)
	}
	protected[m.profile()] = profile
	state, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = state.Put("sessions", "old-session", protocol.Session{ID: "old-session", DeviceID: c.DeviceID, ProjectID: "project", EngineID: "old-thread", EngineStarted: true, State: "unknown", Frozen: true, LastStartID: "old-start", TurnID: "old-turn"}); err != nil {
		t.Fatal(err)
	}
	state.Close()
	for name, raw := range map[string]string{"node-token": "synthetic network credential", "engine-home/auth.json": "synthetic private account state, never loaded by cleanup"} {
		path := filepath.Join(c.DataDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		protected[path] = []byte(raw)
	}
	return m, c, func(value string) { boot = value }, protected
}
func cleanupRequest(op, id, device string) Request {
	return Request{Operation: op, ExpectedDeviceID: device, Cleanup: &CleanupRequest{ID: id}}
}
func assertPreserved(t *testing.T, files map[string][]byte) {
	t.Helper()
	for path, expected := range files {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("recovery changed protected state %s: %v", path, err)
		}
	}
}

func TestCleanupNeedsANewBootAndPreservesTasksCredentialsAndProfile(t *testing.T) {
	m, c, setBoot, files := cleanupFixture(t)
	ctx := context.Background()
	id := protocol.ID()
	prepared, err := m.Handle(ctx, cleanupRequest("cleanup.prepare", id, c.DeviceID))
	if err != nil || prepared.Cleanup == nil || !prepared.Cleanup.NeedsRestart || prepared.Cleanup.CanApply {
		t.Fatal(prepared, err)
	}
	if _, err = m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID)); err == nil {
		t.Fatal("same-boot cleanup was accepted")
	}
	assertPreserved(t, files)
	if _, err = os.Stat(m.cleanupIntentPath(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("same-boot refusal wrote a commit intent")
	}
	read, err := m.Handle(ctx, Request{Operation: "cleanup.status"})
	if err != nil || read.Cleanup.ID != id || read.Cleanup.CanApply {
		t.Fatal("reopen lost its prepared review", read, err)
	}
	setBoot(cleanupBootB)
	read, err = m.Handle(ctx, Request{Operation: "cleanup.status"})
	if err != nil || !read.Cleanup.CanApply || read.Cleanup.NeedsRestart {
		t.Fatal(read, err)
	}
	applied, err := m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID))
	if err != nil || !applied.Cleanup.Applied || applied.Snapshot.State != "stopped" {
		t.Fatal(applied, err)
	}
	assertPreserved(t, files)
	state, openErr := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	var preserved protocol.Session
	if err = state.Get("sessions", "old-session", &preserved); err != nil || preserved.State != "unknown" || !preserved.Frozen || preserved.TurnID != "old-turn" {
		t.Fatal("task outcome changed during capture", preserved, err)
	}
	view, readErr := state.ExecutionRecovery(preserved)
	state.Close()
	if readErr != nil || view == nil || view.State != "eligible" {
		t.Fatal("verified boot did not capture the old attempt", view, readErr)
	}
	before, err := os.ReadFile(m.cleanupIntentPath(id))
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID))
	after, readErr := os.ReadFile(m.cleanupIntentPath(id))
	if err != nil || readErr != nil || !repeated.Cleanup.Applied || !bytes.Equal(before, after) {
		t.Fatal("lost-response retry rewrote its outcome", err, readErr)
	}
	for _, dir := range []string{filepath.Join(m.options.Root, "installation"), c.DataDir} {
		lease, err := lifecycle.Acquire(dir)
		if err != nil {
			t.Fatal(err)
		}
		lease.Close()
	}
}

func TestCleanupRefusesLivingLeasesAndUnavailableBootProof(t *testing.T) {
	m, c, setBoot, _ := cleanupFixture(t)
	ctx := context.Background()
	id := protocol.ID()
	if _, err := m.Handle(ctx, cleanupRequest("cleanup.prepare", id, c.DeviceID)); err != nil {
		t.Fatal(err)
	}
	setBoot(cleanupBootB)
	lease, err := lifecycle.Review(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	read, err := m.Handle(ctx, cleanupRequest("cleanup.status", id, c.DeviceID))
	if err != nil || read.Cleanup.State != "in-use" || read.Cleanup.CanApply {
		t.Fatal(read, err)
	}
	if _, err = m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID)); err == nil {
		t.Fatal("living data lease was ignored")
	}
	lease.Close()
	m.options.BootSession = func() (string, error) { return "", errors.New("boot proof unavailable") }
	read, err = m.Handle(ctx, cleanupRequest("cleanup.status", id, c.DeviceID))
	if err != nil || read.Cleanup.State != "unavailable" || read.Cleanup.CanApply {
		t.Fatal(read, err)
	}
}

func TestCleanupChangedProfileOrLifetimeCannotAdoptAnOldPlan(t *testing.T) {
	for _, change := range []string{"profile", "marker"} {
		t.Run(change, func(t *testing.T) {
			m, c, setBoot, _ := cleanupFixture(t)
			ctx := context.Background()
			id := protocol.ID()
			if _, err := m.Handle(ctx, cleanupRequest("cleanup.prepare", id, c.DeviceID)); err != nil {
				t.Fatal(err)
			}
			setBoot(cleanupBootB)
			if change == "profile" {
				c.Name = "changed after review"
				if err := replaceJSON(m.profile(), c); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(c.DataDir, lifecycle.OwnershipPending), []byte("a-new-unconfirmed-service-lifetime"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID)); err == nil {
				t.Fatal("changed source adopted the old review")
			}
			if _, err := os.Stat(filepath.Join(c.DataDir, lifecycle.OwnershipPending)); err != nil {
				t.Fatal("refusal marker was removed", err)
			}
		})
	}
}

func TestCleanupInterruptedClearResumesOnlyItsJournaledLifetime(t *testing.T) {
	m, c, setBoot, files := cleanupFixture(t)
	ctx := context.Background()
	id := protocol.ID()
	if _, err := m.Handle(ctx, cleanupRequest("cleanup.prepare", id, c.DeviceID)); err != nil {
		t.Fatal(err)
	}
	record, err := m.loadCleanup(id)
	if err != nil {
		t.Fatal(err)
	}
	setBoot(cleanupBootB)
	intent := cleanupIntent{Format: 1, PlanSHA256: hashBytes(protocol.JSON(record)), BootSession: cleanupBootB, State: "committing"}
	if err = createJSON(m.cleanupIntentPath(id), intent); err != nil {
		t.Fatal(err)
	}
	if err = lifecycle.CompleteOwnership([]string{filepath.Join(m.options.Root, "installation")}, record.Markers[0].ID); err != nil {
		t.Fatal(err)
	}
	applied, err := m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID))
	if err != nil || !applied.Cleanup.Applied || applied.Snapshot.State != "stopped" {
		t.Fatal(applied, err)
	}
	assertPreserved(t, files)
}

func TestCleanupMissingMarkersWithoutAnIntentAreNotReportedAsApplied(t *testing.T) {
	m, c, setBoot, _ := cleanupFixture(t)
	ctx := context.Background()
	id := protocol.ID()
	if _, err := m.Handle(ctx, cleanupRequest("cleanup.prepare", id, c.DeviceID)); err != nil {
		t.Fatal(err)
	}
	setBoot(cleanupBootB)
	for _, dir := range []string{filepath.Join(m.options.Root, "installation"), c.DataDir} {
		if err := os.Remove(filepath.Join(dir, lifecycle.OwnershipPending)); err != nil {
			t.Fatal(err)
		}
	}
	read, err := m.Handle(ctx, cleanupRequest("cleanup.status", id, c.DeviceID))
	if err != nil || read.Cleanup.Applied || read.Cleanup.CanApply || read.Cleanup.State != "changed" {
		t.Fatal(read, err)
	}
}

func TestCleanupCancellationBeforeCommitRetainsTheFreeze(t *testing.T) {
	m, c, setBoot, _ := cleanupFixture(t)
	id := protocol.ID()
	if _, err := m.Handle(context.Background(), cleanupRequest("cleanup.prepare", id, c.DeviceID)); err != nil {
		t.Fatal(err)
	}
	setBoot(cleanupBootB)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.DataDir, lifecycle.OwnershipPending)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.cleanupIntentPath(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled operation wrote an intent", err)
	}
}
