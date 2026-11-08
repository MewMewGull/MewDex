package computeruse

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type fixtureReservation struct {
	interrupted chan struct{}
	closed      atomic.Bool
}

func (r *fixtureReservation) Interrupted() <-chan struct{} { return r.interrupted }
func (r *fixtureReservation) Close() error                 { r.closed.Store(true); return nil }

type fixtureDriver struct {
	reservation *fixtureReservation
	actions     atomic.Int32
	entered     chan struct{}
	release     chan struct{}
}

func (d *fixtureDriver) Available() (bool, string) { return true, "" }
func (d *fixtureDriver) Windows(context.Context) ([]protocol.ComputerWindow, error) {
	return []protocol.ComputerWindow{{ID: "ab", ProcessID: 1, Title: "Owned fixture window"}}, nil
}
func (d *fixtureDriver) Reserve(context.Context) (Reservation, error) { return d.reservation, nil }
func (d *fixtureDriver) Observe(ctx context.Context, _ protocol.ComputerWindow) (protocol.ComputerObservation, Bounds, error) {
	return protocol.ComputerObservation{Width: 400, Height: 300, Image: "fixture", MimeType: "image/jpeg"}, Bounds{Width: 400, Height: 300}, ctx.Err()
}
func (d *fixtureDriver) Act(ctx context.Context, _ protocol.ComputerWindow, _ protocol.ComputerObservation, _ Bounds, _ protocol.ComputerAction) error {
	d.actions.Add(1)
	if d.entered != nil {
		close(d.entered)
		<-ctx.Done()
		<-d.release
		return ctx.Err()
	}
	return nil
}

func computerFixture(t *testing.T) (*Manager, *store.Store, *fixtureDriver) {
	t.Helper()
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	driver := &fixtureDriver{reservation: &fixtureReservation{interrupted: make(chan struct{}, 1)}}
	manager, err := New(context.Background(), database, "node", driver)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close(); database.Close() })
	return manager, database, driver
}

func approveFixture(t *testing.T, manager *Manager, database *store.Store, driver *fixtureDriver) protocol.ComputerSession {
	t.Helper()
	status, err := manager.Request("conversation", "project", "control", "Operate only the owned fixture window")
	if err != nil {
		t.Fatal(err)
	}
	windows, _ := driver.Windows(context.Background())
	value, err := Approve(database, protocol.ComputerApproval{SessionID: status.Session.ID, ExpectedRevision: status.Session.Revision, WindowID: "ab", DurationSeconds: 60}, windows)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestWindowControlRequiresExplicitGrantAndFreshObservations(t *testing.T) {
	m, database, driver := computerFixture(t)
	status, err := m.Request("conversation", "project", "control", "Owned window task")
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.ComputerRequest{OwnerID: "conversation", SessionID: "control"}
	if _, err = m.Operate(context.Background(), request, "before-grant"); err == nil {
		t.Fatal("pending request observed a window")
	}
	window, _ := driver.Windows(context.Background())
	approval := protocol.ComputerApproval{SessionID: "control", ExpectedRevision: status.Session.Revision, WindowID: "foreign-window", DurationSeconds: 60}
	if _, err = Approve(database, approval, window); err == nil {
		t.Fatal("foreign window was approved")
	}
	approval.WindowID = "ab"
	if _, err = Approve(database, approval, window); err != nil {
		t.Fatal(err)
	}
	if _, err = Approve(database, approval, window); err == nil {
		t.Fatal("stale local consent was reused")
	}
	first, err := m.Operate(context.Background(), request, "observe")
	if err != nil || first.Observation == nil {
		t.Fatal(first, err)
	}
	request.Action = &protocol.ComputerAction{Kind: "type", Text: "原始 文本\n😀"}
	request.ObservationID = first.Observation.ID
	result, err := m.Operate(context.Background(), request, "type")
	if err != nil || result.Status.Session.LastActionState != "completed" || driver.actions.Load() != 1 {
		t.Fatal(result, err)
	}
	if _, err = m.Operate(context.Background(), request, "stale"); err == nil || driver.actions.Load() != 1 {
		t.Fatal("stale screenshot caused input")
	}
	request.OwnerID = "another-conversation"
	if _, err = m.Operate(context.Background(), request, "foreign"); err == nil {
		t.Fatal("another conversation used the grant")
	}
	if _, err = m.Request("other", "project", "other", "another owner"); err == nil {
		t.Fatal("desktop reservation was replaced")
	}
}

func TestCoordinatesAndTextRequireCompleteBoundedInput(t *testing.T) {
	zero, edge := 0, 10
	observation := protocol.ComputerObservation{Width: 10, Height: 10}
	for _, action := range []protocol.ComputerAction{{Kind: "click"}, {Kind: "click", X: &zero}, {Kind: "click", X: &edge, Y: &zero}, {Kind: "scroll", X: &zero, Y: &zero}, {Kind: "type", Text: "bad\x1b"}} {
		if validateAction(action, observation) == nil {
			t.Fatal("incomplete or out-of-range input was accepted", action)
		}
	}
	if err := validateAction(protocol.ComputerAction{Kind: "click", X: &zero, Y: &zero}, observation); err != nil {
		t.Fatal("explicit zero coordinate was rejected", err)
	}
}

func TestUnusedGrantExpiresAndReleasesTheReservation(t *testing.T) {
	m, database, driver := computerFixture(t)
	value := approveFixture(t, m, database, driver)
	value.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	if _, err := database.SaveComputerSession(value, value.Revision); err != nil {
		t.Fatal(err)
	}
	status, err := m.Status()
	if err != nil || status.Session.State != "expired" {
		t.Fatal(status, err)
	}
	if _, err = m.Request("next-conversation", "project", "next-control", "Next explicit request"); err != nil {
		t.Fatal(err)
	}
}

func TestStopWaitsForOwnedWorkerAndPreservesUnknownOutcome(t *testing.T) {
	m, database, driver := computerFixture(t)
	approveFixture(t, m, database, driver)
	request := protocol.ComputerRequest{OwnerID: "conversation", SessionID: "control"}
	observed, err := m.Operate(context.Background(), request, "observe")
	if err != nil {
		t.Fatal(err)
	}
	driver.entered, driver.release = make(chan struct{}), make(chan struct{})
	request.Action, request.ObservationID = &protocol.ComputerAction{Kind: "type", Text: "fixture"}, observed.Observation.ID
	done := make(chan error, 1)
	go func() { _, err := m.Operate(context.Background(), request, "uncertain-input"); done <- err }()
	<-driver.entered
	stopped := make(chan error, 1)
	go func() { _, err := m.Stop(context.Background(), "control", "conversation"); stopped <- err }()
	select {
	case err := <-stopped:
		t.Fatal("stop returned before its native worker exited", err)
	case <-time.After(30 * time.Millisecond):
	}
	if driver.reservation.closed.Load() {
		t.Fatal("desktop mutex released while native input remained live")
	}
	close(driver.release)
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = <-stopped; err != nil {
		t.Fatal(err)
	}
	status, err := m.Status()
	if err != nil || status.Session.State != "stopped" || status.Session.LastActionID != "uncertain-input" || status.Session.LastActionState != "unknown" || !driver.reservation.closed.Load() {
		t.Fatal(status, err)
	}
	if _, err = m.Operate(context.Background(), request, "late"); err == nil {
		t.Fatal("late input survived stop")
	}
}

func TestPhysicalStopExpiryDisconnectAndRestartRequireNewConsent(t *testing.T) {
	for _, operation := range []string{"escape", "expiry", "disconnect", "restart"} {
		t.Run(operation, func(t *testing.T) {
			m, database, driver := computerFixture(t)
			approveFixture(t, m, database, driver)
			if _, err := m.Operate(context.Background(), protocol.ComputerRequest{OwnerID: "conversation", SessionID: "control"}, "observe"); err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "escape":
				driver.reservation.interrupted <- struct{}{}
			case "expiry":
				value, _ := database.ComputerSession()
				value.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
				if _, err := database.SaveComputerSession(value, value.Revision); err != nil {
					t.Fatal(err)
				}
			case "disconnect":
				m.Interrupt("connection ended")
			case "restart":
				m.Close()
				value, _ := database.ComputerSession()
				value.State, value.LastActionState = "ready", "running"
				// Seed the persisted crash state independently of the closed session.
				if err := database.Put("computer_sessions", value.ID, value); err != nil {
					t.Fatal(err)
				}
				restarted, err := New(context.Background(), database, "node", driver)
				if err != nil {
					t.Fatal(err)
				}
				defer restarted.Close()
				status, _ := restarted.Status()
				if status.Session.State != "unknown" || status.Session.LastActionState != "unknown" {
					t.Fatal(status)
				}
				if _, err = restarted.Operate(context.Background(), protocol.ComputerRequest{OwnerID: "conversation", SessionID: "control"}, "replay"); err == nil {
					t.Fatal("restart restored a window grant")
				}
				return
			}
			deadline := time.After(2 * time.Second)
			for {
				status, _ := m.Status()
				if status.Session.State == "stopped" {
					break
				}
				select {
				case <-deadline:
					t.Fatal("stop did not settle", status)
				case <-time.After(10 * time.Millisecond):
				}
			}
			if !driver.reservation.closed.Load() {
				t.Fatal("native reservation remained owned")
			}
		})
	}
}
