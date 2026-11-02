package store

import (
	"context"
	"testing"

	"mewdex/internal/protocol"
)

func restartEvidence() RestartEvidence {
	return RestartEvidence{ID: "recovery", DeviceID: "node", BootSession: "boot-after", RestartFrom: "boot-before", PlanSHA256: "bound-local-plan"}
}

func TestRestartSnapshotCannotCaptureNewAttemptsOnRetryOrGrantCurrentBootWork(t *testing.T) {
	s := memory(t)
	ctx := context.Background()
	old := protocol.Session{ID: "old", DeviceID: "node", ProjectID: "p", EngineID: "old-thread", LastStartID: "old-start", TurnID: "old-turn", State: "unknown", Frozen: true}
	fresh := protocol.Session{ID: "fresh", DeviceID: "node", ProjectID: "p", EngineID: "fresh-thread", LastStartID: "fresh-start", State: "unknown", Frozen: true}
	for _, value := range []protocol.Session{old, fresh} {
		if err := s.Put("sessions", value.ID, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordExecutionOrigin(fresh, "boot-after"); err != nil {
		t.Fatal(err)
	}
	// The origin is persisted before turn/start returns its turn identifier.
	fresh.TurnID = "new-turn"
	if err := s.Put("sessions", fresh.ID, fresh); err != nil {
		t.Fatal(err)
	}
	evidence := restartEvidence()
	if err := s.CaptureRestartRecovery(ctx, evidence); err != nil {
		t.Fatal(err)
	}
	view, err := s.ExecutionRecovery(old)
	if err != nil || view == nil || view.State != "eligible" {
		t.Fatal(view, err)
	}
	if view, err := s.ExecutionRecovery(fresh); err != nil || view != nil {
		t.Fatal("current-boot work inherited an old proof", view, err)
	}
	later := old
	later.ID = "later"
	later.LastStartID = "later-start"
	if err = s.Put("sessions", later.ID, later); err != nil {
		t.Fatal(err)
	}
	if err = s.CaptureRestartRecovery(ctx, evidence); err != nil {
		t.Fatal(err)
	}
	if view, err := s.ExecutionRecovery(later); err != nil || view != nil {
		t.Fatal("retry enrolled work created after the original snapshot", view, err)
	}
	old.TurnID, old.State, old.StopStatus = "", "paused", "processes-unchecked"
	if view, err := s.ExecutionRecovery(old); err != nil || view == nil || view.State != "eligible" {
		t.Fatal("read-only reconciliation invalidated the original attempt proof", view, err)
	}
	old.LastStartID = "new-attempt"
	if view, err := s.ExecutionRecovery(old); err != nil || view != nil {
		t.Fatal("old proof matched a new start", view, err)
	}
}

func TestStartupRecoversOnlyTheRetirementControlReceipt(t *testing.T) {
	s := memory(t)
	old := protocol.Session{ID: "old", DeviceID: "node", ProjectID: "p", EngineID: "thread", LastStartID: "start", TurnID: "turn", State: "unknown", Frozen: true}
	if err := s.Put("sessions", old.ID, old); err != nil {
		t.Fatal(err)
	}
	if err := s.CaptureRestartRecovery(context.Background(), restartEvidence()); err != nil {
		t.Fatal(err)
	}
	view, err := s.ExecutionRecovery(old)
	if err != nil || view == nil {
		t.Fatal(view, err)
	}
	control := protocol.Command{ID: "retire", DeviceID: old.DeviceID, SessionID: old.ID, SourceID: "phone", Kind: "execution.retire", RequestID: view.ID}
	original := protocol.Command{ID: old.LastStartID, DeviceID: old.DeviceID, SessionID: old.ID, SourceID: "phone", Kind: "start"}
	withoutReceipt := control
	withoutReceipt.ID = "never-applied"
	for _, command := range []protocol.Command{control, original, withoutReceipt} {
		if _, _, err := s.Claim(command); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate losing the process between the durable retirement and Finish.
	retired, _, err := s.RetireExecution(old, control)
	if err != nil || retired.ExecutionRecovery.State != "retired" {
		t.Fatal(retired, err)
	}
	if _, err = s.RecoverCommands(old.DeviceID); err != nil {
		t.Fatal(err)
	}
	for id, expected := range map[string]string{control.ID: "completed", original.ID: "unknown", withoutReceipt.ID: "unknown"} {
		result, err := s.Result(id)
		if err != nil || result.State != expected {
			t.Fatal("receipt recovered without its own evidence", id, result, err)
		}
	}
	if err = s.PrepareRestore(context.Background(), old.DeviceID, true); err != nil {
		t.Fatal(err)
	}
	if receipts, err := s.List("execution_retirement_receipts"); err != nil || len(receipts) != 0 {
		t.Fatal("restore retained action authorization", err)
	}
	if view, err := s.ExecutionRecovery(retired); err != nil || view == nil || view.State != "retired" {
		t.Fatal("restore reopened retired historical work", view, err)
	}
}

func TestRestartSnapshotRollsBackAcrossForeignDeviceAndRestoreRevokesEligibility(t *testing.T) {
	s := memory(t)
	ctx := context.Background()
	old := protocol.Session{ID: "old", DeviceID: "node", State: "unknown", EngineID: "thread"}
	if err := s.Put("sessions", old.ID, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("sessions", "foreign", protocol.Session{ID: "foreign", DeviceID: "other"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CaptureRestartRecovery(ctx, restartEvidence()); err == nil {
		t.Fatal("foreign device was accepted")
	}
	if values, err := s.List("execution_recovery_candidates"); err != nil || len(values) != 0 {
		t.Fatal("failed snapshot leaked candidates", err)
	}
	if err := s.Delete("sessions", "foreign"); err != nil {
		t.Fatal(err)
	}
	if err := s.CaptureRestartRecovery(ctx, restartEvidence()); err != nil {
		t.Fatal(err)
	}
	old.ExecutionRecovery = &protocol.ExecutionRecovery{ID: "untrusted-import", State: "eligible"}
	if err := s.Put("sessions", old.ID, old); err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareRestore(ctx, "node", true); err != nil {
		t.Fatal(err)
	}
	old = protocol.Session{} // Decode the restored object, not a prior optional pointer.
	if err := s.Get("sessions", "old", &old); err != nil {
		t.Fatal(err)
	}
	if old.ExecutionRecovery != nil {
		t.Fatal("restored projection retained authorization")
	}
	if view, err := s.ExecutionRecovery(old); err != nil || view != nil {
		t.Fatal("restored proof remained active", view, err)
	}
}
