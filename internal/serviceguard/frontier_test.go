package serviceguard

import (
	"testing"

	"mewdex/internal/processidentity"
)

func birth(pid int, id, parent uint64) processidentity.Identity {
	return processidentity.Identity{PID: pid, UniqueID: id, ParentUniqueID: parent, PIDVersion: 1, UID: 501, State: 2}
}

func TestNewOrphanWithMissingBirthParentRemainsUnknownAfterReparenting(t *testing.T) {
	owner, unrelated := birth(10, 100, 1), birth(20, 200, 999)
	owned := map[uint64]processidentity.Identity{owner.UniqueID: owner}
	var frontier outsideFrontier
	if unknown, err := frontier.observe(owned, []processidentity.Identity{owner, unrelated}); err != nil || unknown != 0 {
		t.Fatal(unknown, err)
	}
	child := birth(21, 201, 200)
	grandchild := birth(22, 202, 201)
	if unknown, err := frontier.observe(owned, []processidentity.Identity{owner, child, grandchild}); err != nil || unknown != 0 {
		t.Fatal("outside ancestry was lost when its original parent exited", unknown, err)
	}
	orphan := birth(30, 300, 299)
	orphan.ParentPID = 1
	for i := 0; i < 3; i++ {
		if unknown, err := frontier.observe(owned, []processidentity.Identity{owner, child, grandchild, orphan}); err != nil || unknown != 1 {
			t.Fatal("repeated snapshots silently adopted an unknown orphan", unknown, err)
		}
	}
}

func TestOutsidePIDReuseDoesNotMakeANewUnknownLifetimeUnrelated(t *testing.T) {
	owner, other := birth(10, 100, 1), birth(20, 200, 1)
	owned := map[uint64]processidentity.Identity{100: owner}
	var frontier outsideFrontier
	_, _ = frontier.observe(owned, []processidentity.Identity{owner, other})
	reused := birth(other.PID, 300, 299)
	if unknown, err := frontier.observe(owned, []processidentity.Identity{owner, reused}); err != nil || unknown != 1 {
		t.Fatal("outside PID reuse transferred an old lifetime's classification", unknown, err)
	}
}

func TestOutsideAncestryDoesNotCrossAnOwnedBoundary(t *testing.T) {
	launcher, owner, child := birth(10, 100, 1), birth(20, 200, 100), birth(30, 300, 200)
	var frontier outsideFrontier
	owned := map[uint64]processidentity.Identity{200: owner, 300: child}
	if unknown, err := frontier.observe(owned, []processidentity.Identity{launcher, owner, child}); err != nil || unknown != 0 {
		t.Fatal(unknown, err)
	}
	if frontier.current[owner.UniqueID] || frontier.current[child.UniqueID] {
		t.Fatal("outside traversal crossed into an owned service boundary")
	}
}
