package store

import (
	"errors"
	"sync"
	"testing"

	"mewdex/internal/protocol"
)

func TestComputerReservationUsesAtomicRevisionsAndRetainsHistory(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	first := protocol.ComputerSession{ID: "first", DeviceID: "node", ProjectID: "root", OwnerID: "conversation", InstanceID: "instance", State: "pending", CreatedAt: protocol.Now()}
	first, err = database.SaveComputerSession(first, 0)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for _, state := range []string{"approved", "stopped"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			next := first
			next.State = state
			_, err := database.SaveComputerSession(next, first.Revision)
			results <- err
		}()
	}
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	current, err := database.ComputerSession()
	if err != nil {
		t.Fatal(err)
	}
	foreign := current
	foreign.OwnerID = "foreign"
	if _, err = database.SaveComputerSession(foreign, current.Revision); err == nil {
		t.Fatal("reservation owner changed")
	}
	current.State = "stopped"
	current.LastActionState = "unknown"
	current, err = database.SaveComputerSession(current, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	resumed := current
	resumed.State = "ready"
	if _, err = database.SaveComputerSession(resumed, current.Revision); err == nil {
		t.Fatal("finished grant was reactivated")
	}
	next := first
	next.ID = "second"
	next.State = "pending"
	next.CreatedAt = protocol.Now()
	next, err = database.SaveComputerSession(next, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	var original protocol.ComputerSession
	if err = database.Get("computer_sessions", first.ID, &original); err != nil || original.LastActionState != "unknown" || original.State != "stopped" {
		t.Fatal(original, err)
	}
	next.State = "stopped"
	next, err = database.SaveComputerSession(next, next.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.SaveComputerSession(first, next.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("historical session identity was reused", err)
	}
}
