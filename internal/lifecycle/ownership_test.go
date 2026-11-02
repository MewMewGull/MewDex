package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnconfirmedOwnershipCannotBeReacquiredOrClearedByAnotherLifetime(t *testing.T) {
	dir := t.TempDir()
	lease, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := "new-service-lifetime-123456789"
	if err = BeginOwnership([]*Lock{lease}, id); err != nil {
		t.Fatal(err)
	}
	lease.Close()
	if other, err := Acquire(dir); !errors.Is(err, ErrUnconfirmed) {
		if other != nil {
			other.Close()
		}
		t.Fatalf("unconfirmed lifetime was silently adopted: %v", err)
	}
	if err = CompleteOwnership([]string{dir}, "another-service-lifetime"); !errors.Is(err, ErrUnconfirmed) {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, OwnershipPending)); err != nil || string(raw) != id {
		t.Fatal("the original refusal marker changed")
	}
	if err = CompleteOwnership([]string{dir}, id); err != nil {
		t.Fatal(err)
	}
	lease, err = Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
}
