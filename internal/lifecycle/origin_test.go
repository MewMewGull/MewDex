package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOriginBindsTheOSBootAndRetainsItsAuditAfterCleanup(t *testing.T) {
	dir := t.TempDir()
	lease, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := "new-origin-fixture-lifetime-012345"
	if err = BeginOwnership([]*Lock{lease}, id); err != nil {
		t.Fatal(err)
	}
	marker, err := ReadOwnership(dir)
	if err != nil || marker == nil || marker.ID != id {
		t.Fatal(marker, err)
	}
	boot, bootErr := BootSession()
	if bootErr == nil && (marker.BootSession != boot || marker.Legacy) {
		t.Fatal("origin did not bind the actual OS boot")
	}
	if bootErr != nil && (!marker.Legacy || marker.BootSession != "") {
		t.Fatal("unsupported boot identity invented proof")
	}
	origin, err := os.ReadFile(originPath(dir, id))
	if err != nil {
		t.Fatal(err)
	}
	if err = CompleteOwnership([]string{dir}, id); err != nil {
		t.Fatal(err)
	}
	lease.Close()
	if _, err = os.Stat(filepath.Join(dir, OwnershipPending)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(originPath(dir, id))
	if err != nil || string(retained) != string(origin) {
		t.Fatal("cleanup replaced its origin evidence", err)
	}
}

func TestLegacyOriginNeedsABootCheckpointAndChangesItsFingerprintOnMetadataArrival(t *testing.T) {
	dir := t.TempDir()
	id := "legacy-origin-fixture-lifetime"
	if err := os.WriteFile(filepath.Join(dir, OwnershipPending), []byte(id), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := ReadOwnership(dir)
	if err != nil || !before.Legacy || before.BootSession != "" {
		t.Fatal(before, err)
	}
	if err = createOwnershipOrigin(dir, ownershipOrigin{Format: 1, ID: id, BootSession: "11111111-1111-4111-8111-111111111111"}); err != nil {
		t.Fatal(err)
	}
	after, err := ReadOwnership(dir)
	if err != nil || after.Legacy || after.Fingerprint == before.Fingerprint {
		t.Fatal("new origin did not invalidate an earlier review", after, err)
	}
}
