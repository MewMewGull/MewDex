package serviceguard

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/lifecycle"
)

func TestNestedEntryRetainsDataLeaseAndRejectsScopeChanges(t *testing.T) {
	dir := t.TempDir()
	err := Run(context.Background(), dir, nil, false, func(ctx context.Context) error {
		if lease, err := lifecycle.Acquire(dir); err == nil {
			lease.Close()
			t.Fatal("callback lost its data lease")
		}
		if err := Run(ctx, filepath.Join(dir, "other"), nil, false, func(context.Context) error { t.Fatal("changed data scope was executed"); return nil }); err == nil {
			t.Fatal("scope change was accepted")
		}
		return Run(ctx, dir, nil, false, func(context.Context) error { return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := lifecycle.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
}

func TestActionPanicDoesNotRetainTheDataLease(t *testing.T) {
	dir := t.TempDir()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("action panic was swallowed")
			}
		}()
		_ = Run(context.Background(), dir, nil, false, func(context.Context) error { panic("fixture") })
	}()
	lease, err := lifecycle.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	lease.Close()
	if _, err := os.Stat(filepath.Join(dir, lifecycle.OwnershipPending)); !os.IsNotExist(err) {
		t.Fatal("Hub-only scope created a process supervisor")
	}
}
