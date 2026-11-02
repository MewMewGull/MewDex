package processidentity

import (
	"errors"
	"testing"
)

func identity(pid int, unique, parent uint64) Identity {
	return Identity{PID: pid, UniqueID: unique, ParentUniqueID: parent, PIDVersion: 1}
}

func TestReparentedDescendantAndReusedRootPID(t *testing.T) {
	root := identity(20, 100, 50)
	child := identity(21, 101, 100)
	child.ParentPID = 1
	grandchild := identity(22, 102, 101)
	unrelated := identity(20, 999, 900)
	found, err := Discover([]Identity{root}, []Identity{grandchild, unrelated, child})
	if err != nil || len(found) != 2 {
		t.Fatalf("reparented chain: %#v, %v", found, err)
	}
	for _, value := range found {
		if value.UniqueID == unrelated.UniqueID {
			t.Fatal("reused PID acquired ownership")
		}
	}
	if root.SameLifetime(unrelated) {
		t.Fatal("PID reuse was mistaken for the original lifetime")
	}
}

func TestMissingBirthParentIsNotInferred(t *testing.T) {
	root := identity(20, 100, 50)
	unknown := identity(21, 102, 101)
	unknown.ParentPID, unknown.Group = root.PID, root.PID
	found, err := Discover([]Identity{root}, []Identity{unknown})
	if err != nil || len(found) != 0 {
		t.Fatalf("an unobserved intermediate was inferred: %#v, %v", found, err)
	}
}

func TestInvalidIdentityCannotSeedOwnership(t *testing.T) {
	if _, err := Discover([]Identity{{PID: 20}}, []Identity{identity(21, 101, 0)}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := Discover([]Identity{identity(20, 100, 50)}, []Identity{identity(21, 101, 100), identity(22, 101, 100)}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := Discover([]Identity{identity(20, 100, 50)}, []Identity{identity(21, 100, 50)}); !errors.Is(err, ErrInvalid) {
		t.Fatal("contradictory root identity was accepted", err)
	}
}

func TestExecGenerationDoesNotTransferLifetimeOwnership(t *testing.T) {
	before := identity(20, 100, 50)
	after := before
	after.PIDVersion++
	if !before.SameLifetime(after) {
		t.Fatal("exec was treated as a new process lifetime")
	}
}
