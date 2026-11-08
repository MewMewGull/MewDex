package executor

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"mewdex/internal/computeruse"
	"mewdex/internal/protocol"
)

type computerFixtureDriver struct{ actions atomic.Int32 }
type computerFixtureReservation struct{ stop chan struct{} }

func (r *computerFixtureReservation) Close() error                 { return nil }
func (r *computerFixtureReservation) Interrupted() <-chan struct{} { return r.stop }
func (d *computerFixtureDriver) Available() (bool, string)         { return true, "" }
func (d *computerFixtureDriver) Windows(context.Context) ([]protocol.ComputerWindow, error) {
	return []protocol.ComputerWindow{{ID: "fixture", ProcessID: 1, Title: "Fixture"}}, nil
}
func (d *computerFixtureDriver) Reserve(context.Context) (computeruse.Reservation, error) {
	return &computerFixtureReservation{stop: make(chan struct{})}, nil
}
func (d *computerFixtureDriver) Observe(context.Context, protocol.ComputerWindow) (protocol.ComputerObservation, computeruse.Bounds, error) {
	return protocol.ComputerObservation{Image: "fixture", MimeType: "image/jpeg", Width: 100, Height: 100}, computeruse.Bounds{Width: 100, Height: 100}, nil
}
func (d *computerFixtureDriver) Act(context.Context, protocol.ComputerWindow, protocol.ComputerObservation, computeruse.Bounds, protocol.ComputerAction) error {
	d.actions.Add(1)
	return nil
}

func TestComputerCommandsUseExistingReceiptsAndStoppedTurnBarrier(t *testing.T) {
	x, database, _ := setup(t)
	driver := &computerFixtureDriver{}
	manager, err := computeruse.New(context.Background(), database, x.config.DeviceID, driver)
	if err != nil {
		t.Fatal(err)
	}
	x.computer = manager
	t.Cleanup(manager.Close)
	command := protocol.Command{ID: "request", DeviceID: x.config.DeviceID, ProjectID: x.config.Projects[0].ID, Kind: "computer.request", SourceID: "hub", Computer: &protocol.ComputerRequest{OwnerID: "conversation", TurnID: "turn", Reason: "Owned test window"}}
	call := func(value protocol.Command) protocol.ComputerResult {
		t.Helper()
		result := x.Handle(context.Background(), value)
		if result.State != "completed" {
			t.Fatal(result)
		}
		var parsed protocol.ComputerResult
		if err := json.Unmarshal(result.Data, &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	requested := call(command)
	windows, _ := driver.Windows(context.Background())
	if _, err = computeruse.Approve(database, protocol.ComputerApproval{SessionID: requested.Status.Session.ID, ExpectedRevision: requested.Status.Session.Revision, WindowID: "fixture", DurationSeconds: 60}, windows); err != nil {
		t.Fatal(err)
	}
	command.ID, command.Kind, command.Computer.SessionID = "observe", "computer.observe", requested.Status.Session.ID
	observed := call(command)
	command.ID, command.Kind = "act", "computer.act"
	command.Computer.ObservationID, command.Computer.Action = observed.Observation.ID, &protocol.ComputerAction{Kind: "type", Text: "literal"}
	first := call(command)
	second := call(command)
	if string(protocol.JSON(first)) != string(protocol.JSON(second)) || driver.actions.Load() != 1 {
		t.Fatal("duplicate receipt replayed UI input")
	}
	stop := command
	stop.ID, stop.Kind = "stop", "computer.stop"
	stop.Computer = &protocol.ComputerRequest{OwnerID: "conversation", TurnID: "turn", SessionID: requested.Status.Session.ID}
	if call(stop).Status.Session.State != "stopped" {
		t.Fatal("stop did not finish")
	}
	command.ID = "late-act"
	if x.Handle(context.Background(), command).State != "failed" || driver.actions.Load() != 1 {
		t.Fatal("late input crossed the stop barrier")
	}
	command.Kind, command.ID = "computer.request", "late-request"
	if x.Handle(context.Background(), command).State != "failed" {
		t.Fatal("late request recreated a grant after stopping")
	}
	stop.ID, stop.Computer.TurnID, stop.Computer.SessionID = "early-stop", "next-turn", "not-yet-delivered"
	if x.Handle(context.Background(), stop).State != "failed" {
		t.Fatal("an undelivered session unexpectedly existed")
	}
	command.ID, command.Computer.TurnID = "request-after-early-stop", "next-turn"
	if x.Handle(context.Background(), command).State != "failed" {
		t.Fatal("request crossed an earlier stop without a live session")
	}
	command.Kind, command.ID = "computer.status", "status-foreign"
	command.Computer = &protocol.ComputerRequest{OwnerID: "other-conversation"}
	if call(command).Status.Session != nil {
		t.Fatal("status revealed another conversation's window")
	}
}
