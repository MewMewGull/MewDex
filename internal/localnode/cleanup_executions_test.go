package localnode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestCleanupCanRecoverRestoredUnknownExecutionsWithoutServiceMarkers(t *testing.T) {
	m, c, setBoot, files := cleanupFixture(t)
	ctx := context.Background()
	for _, dir := range []string{filepath.Join(m.options.Root, "installation"), c.DataDir} {
		if err := os.Remove(filepath.Join(dir, lifecycle.OwnershipPending)); err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	queued := protocol.QueueItem{ID: "kept-input", SessionID: "old-session", DeviceID: c.DeviceID, ProjectID: "project", State: "unknown", Revision: 3, Input: []protocol.Input{{Type: "text", Text: "preserve this input without replay"}}}
	if queued, _, err = state.PutQueueItem(queued); err != nil {
		t.Fatal(err)
	}
	if err = state.PrepareRestore(ctx, c.DeviceID, true); err != nil {
		t.Fatal(err)
	}
	if err = state.Close(); err != nil {
		t.Fatal(err)
	}
	id := protocol.ID()
	prepared, err := m.Handle(ctx, cleanupRequest("cleanup.prepare", id, c.DeviceID))
	if err != nil || prepared.Cleanup == nil || !prepared.Cleanup.NeedsRestart || prepared.Cleanup.CanApply {
		t.Fatal("restored unknown execution had no recovery path", prepared, err)
	}
	if _, err = m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID)); err == nil {
		t.Fatal("markerless recovery accepted the original boot")
	}
	setBoot(cleanupBootB)
	lease, err := lifecycle.Review(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID)); err == nil {
		t.Fatal("markerless recovery ignored a living service lease")
	}
	reading, readErr := m.Handle(ctx, cleanupRequest("cleanup.status", id, c.DeviceID))
	if readErr != nil || reading.Cleanup == nil || reading.Cleanup.State != "in-use" {
		t.Fatal("living markerless service lost its review state", reading, readErr)
	}
	lease.Close()
	applied, err := m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID))
	if err != nil || applied.Cleanup == nil || !applied.Cleanup.Applied {
		t.Fatal(applied, err)
	}
	assertPreserved(t, files)
	state, err = store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	var session protocol.Session
	if err = state.Get("sessions", "old-session", &session); err != nil || session.State != "unknown" || session.TurnID != "old-turn" || !session.Frozen {
		t.Fatal("checkpoint rewrote the old result", session, err)
	}
	view, err := state.ExecutionRecovery(session)
	if err != nil || view == nil || view.State != "eligible" {
		t.Fatal("verified markerless checkpoint did not authorize explicit retirement", view, err)
	}
	var kept protocol.QueueItem
	if err = state.Get("queue_items", queued.ID, &kept); err != nil || string(protocol.JSON(kept)) != string(protocol.JSON(queued)) {
		t.Fatal("checkpoint changed preserved input", kept, err)
	}
}

func TestMarkerlessCleanupInterruptedBeforeCaptureIsNotReportedAsApplied(t *testing.T) {
	for _, captured := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-capture", true: "after-capture"}[captured], func(t *testing.T) {
			m, c, setBoot, _ := cleanupFixture(t)
			for _, dir := range []string{filepath.Join(m.options.Root, "installation"), c.DataDir} {
				if err := os.Remove(filepath.Join(dir, lifecycle.OwnershipPending)); err != nil {
					t.Fatal(err)
				}
			}
			ctx, id := context.Background(), protocol.ID()
			if _, err := m.Handle(ctx, cleanupRequest("cleanup.prepare", id, c.DeviceID)); err != nil {
				t.Fatal(err)
			}
			record, err := m.loadCleanup(id)
			if err != nil {
				t.Fatal(err)
			}
			setBoot(cleanupBootB)
			intent := cleanupIntent{Format: 1, PlanSHA256: hashBytes(protocol.JSON(record)), BootSession: cleanupBootB, State: "committing", ExecutionCapture: true}
			if err = createJSON(m.cleanupIntentPath(id), intent); err != nil {
				t.Fatal(err)
			}
			if captured {
				view, err := m.openCleanup()
				if err != nil {
					t.Fatal(err)
				}
				err = m.captureCleanupExecutions(ctx, view, record, intent)
				view.close()
				if err != nil {
					t.Fatal(err)
				}
			}
			read, err := m.Handle(ctx, cleanupRequest("cleanup.status", id, c.DeviceID))
			if err != nil || read.Cleanup == nil || read.Cleanup.Applied || !read.Cleanup.CanApply {
				t.Fatal("an intent alone was treated as finished execution recovery", read, err)
			}
			applied, err := m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID))
			if err != nil || !applied.Cleanup.Applied {
				t.Fatal(applied, err)
			}
			state, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close()
			var session protocol.Session
			if err = state.Get("sessions", "old-session", &session); err != nil {
				t.Fatal(err)
			}
			view, err := state.ExecutionRecovery(session)
			if err != nil || view == nil || view.State != "eligible" {
				t.Fatal("resuming the intent did not persist the certificate", view, err)
			}
		})
	}
}

func TestMarkerlessCleanupRecognizesUnknownQueueBeforeExecutorNormalization(t *testing.T) {
	m, c, _, _ := cleanupFixture(t)
	for _, dir := range []string{filepath.Join(m.options.Root, "installation"), c.DataDir} {
		if err := os.Remove(filepath.Join(dir, lifecycle.OwnershipPending)); err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	var session protocol.Session
	if err = state.Get("sessions", "old-session", &session); err != nil {
		t.Fatal(err)
	}
	session.State, session.StopStatus, session.EngineStarted = "idle", "", false
	if err = state.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	if _, _, err = state.PutQueueItem(protocol.QueueItem{ID: "dispatched-before-copy", SessionID: session.ID, DeviceID: c.DeviceID, ProjectID: session.ProjectID, State: "unknown", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	state.Close()
	read, err := m.Handle(context.Background(), Request{Operation: "cleanup.status"})
	if err != nil || read.Cleanup == nil || !read.Cleanup.CanPrepare || read.Cleanup.Pending || read.Cleanup.UnconfirmedExecutions != 1 {
		t.Fatal("unknown queue could retain a project without a recovery checkpoint", read, err)
	}
}

func TestMarkerlessReviewRemainsVisibleWhileAnInterveningServiceHoldsItsLeases(t *testing.T) {
	m, c, setBoot, _ := cleanupFixture(t)
	dirs := []string{filepath.Join(m.options.Root, "installation"), c.DataDir}
	for _, dir := range dirs {
		if err := os.Remove(filepath.Join(dir, lifecycle.OwnershipPending)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, id := context.Background(), protocol.ID()
	if _, err := m.Handle(ctx, cleanupRequest("cleanup.prepare", id, c.DeviceID)); err != nil {
		t.Fatal(err)
	}
	leases := []*lifecycle.Lock{}
	for _, dir := range dirs {
		lease, err := lifecycle.Acquire(dir)
		if err != nil {
			t.Fatal(err)
		}
		leases = append(leases, lease)
		defer lease.Close()
	}
	owner := "intervening-service-fixture-lifetime"
	if err := lifecycle.BeginOwnership(leases, owner); err != nil {
		t.Fatal(err)
	}
	setBoot(cleanupBootB)
	read, err := m.Handle(ctx, Request{Operation: "cleanup.status"})
	if err != nil || read.Cleanup == nil || read.Cleanup.ID != id || read.Cleanup.State != "in-use" || read.Cleanup.CanApply {
		t.Fatal("running service hid the saved checkpoint", read, err)
	}
	if _, err = m.Handle(ctx, cleanupRequest("cleanup.apply", id, c.DeviceID)); err == nil {
		t.Fatal("running service allowed checkpoint adoption")
	}
	if err = lifecycle.CompleteOwnership(dirs, owner); err != nil {
		t.Fatal(err)
	}
	for _, lease := range leases {
		if err = lease.Close(); err != nil {
			t.Fatal(err)
		}
	}
	read, err = m.Handle(ctx, Request{Operation: "cleanup.status"})
	if err != nil || read.Cleanup == nil || read.Cleanup.ID != id || !read.Cleanup.CanApply {
		t.Fatal("stopped service could not continue the original checkpoint", read, err)
	}
}
