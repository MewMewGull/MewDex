package serviceguard

import (
	"errors"
	"testing"
	"time"

	"mewdex/internal/processidentity"
)

func TestServiceHistoryContinuesBeyondTheCumulativeLifetimeLimit(t *testing.T) {
	var history lifetimeHistory
	owner := birth(10, 100, 1)
	if _, err := history.remember(owner, 16); err != nil {
		t.Fatal(err)
	}
	protected := map[uint64]bool{owner.UniqueID: true}
	now := time.Now()
	for wave := 0; wave < 8200; wave++ {
		for child := 0; child < 8; child++ {
			// Reuse OS PIDs while each kernel birth identity remains distinct.
			identity := birth(20+child, uint64(1000+wave*8+child), owner.UniqueID)
			if _, err := history.remember(identity, 16); err != nil {
				t.Fatal("completed tools exhausted the next wave", wave, err)
			}
		}
		history.trim(now, []processidentity.Identity{owner}, protected)
		now = now.Add(exitedHistoryGrace + time.Millisecond)
		history.trim(now, []processidentity.Identity{owner}, protected)
		if len(history.known) != 1 || len(history.absent) != 0 {
			t.Fatal("terminal history grew with completed waves", len(history.known), len(history.absent))
		}
	}
	if history.total <= trackingLimit || history.retired != 8200*8 || history.peak != 9 {
		t.Fatal(history.total, history.retired, history.peak)
	}
}

func TestRetiringADeadAncestorKeepsItsObservedLiveDescendantsOwned(t *testing.T) {
	var history lifetimeHistory
	owner, parent, detached := birth(10, 100, 1), birth(20, 200, 100), birth(30, 300, 200)
	for _, identity := range []processidentity.Identity{owner, parent, detached} {
		if _, err := history.remember(identity, trackingLimit); err != nil {
			t.Fatal(err)
		}
	}
	detached.ParentPID = 1 // Reparenting does not change its immutable parent.
	snapshot := []processidentity.Identity{owner, detached}
	now := time.Now()
	protected := map[uint64]bool{owner.UniqueID: true}
	history.trim(now, snapshot, protected)
	history.trim(now.Add(exitedHistoryGrace), snapshot, protected)
	if _, kept := history.known[parent.UniqueID]; kept {
		t.Fatal("exited intermediate never retired")
	}
	roots := []processidentity.Identity{}
	for _, identity := range history.known {
		roots = append(roots, identity)
	}
	grandchild := birth(40, 400, detached.UniqueID)
	found, err := processidentity.Discover(roots, append(snapshot, grandchild))
	if err != nil || len(found) != 1 || found[0].UniqueID != grandchild.UniqueID {
		t.Fatal("live detached ancestry was lost", found, err)
	}
	// A late unknown lifetime cannot use the retired parent's old OS PID as
	// ownership or be silently reclassified as an unrelated process.
	var outside outsideFrontier
	if _, err := outside.observe(history.known, snapshot); err != nil {
		t.Fatal(err)
	}
	orphan := birth(parent.PID, 500, parent.UniqueID)
	orphan.ParentPID = 1
	if unknown, err := outside.observe(history.known, append(snapshot, orphan)); err != nil || unknown != 1 {
		t.Fatal("retirement hid missing ancestry", unknown, err)
	}
}

func TestLiveFiltersAndUnresolvedForksCannotBeAgedOutToBypassTheBound(t *testing.T) {
	var history lifetimeHistory
	owner, child, pending := birth(10, 100, 1), birth(20, 200, 100), birth(30, 300, 100)
	for _, identity := range []processidentity.Identity{owner, child, pending} {
		if _, err := history.remember(identity, 3); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	protected := map[uint64]bool{owner.UniqueID: true, child.UniqueID: true, pending.UniqueID: true}
	history.trim(now, nil, protected)
	history.trim(now.Add(100*exitedHistoryGrace), nil, protected)
	if _, err := history.remember(birth(40, 400, 100), 3); !errors.Is(err, processidentity.ErrLimit) {
		t.Fatal("unresolved identities evicted to admit more work", err)
	}
	changed := child
	changed.PID++
	if _, err := history.remember(changed, 3); !errors.Is(err, processidentity.ErrInvalid) {
		t.Fatal("one birth identity transferred to another PID", err)
	}
}
