//go:build darwin

package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInheritedOpenDescriptionRetainsTheOSLeaseThroughLastClose(t *testing.T) {
	dir := t.TempDir()
	lease, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := lease.Inherit()
	if err != nil {
		lease.Close()
		t.Fatal(err)
	}
	defer duplicate.Close()
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	probe, err := os.OpenFile(filepath.Join(dir, "service.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err = unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("original owner close released the inherited lease: %v", err)
	}
	if err = duplicate.Close(); err != nil {
		t.Fatal(err)
	}
	if err = unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal("last inherited close did not release the lease", err)
	}
}
