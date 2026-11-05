//go:build darwin

package serviceguard

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"mewdex/internal/processidentity"
)

func TestUnrelatedMissingAncestryDoesNotStopService(t *testing.T) {
	ownerProcess := exec.Command("/bin/sleep", "30")
	if err := ownerProcess.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ownerProcess.Process.Kill(); _ = ownerProcess.Wait() })
	owner, err := processidentity.Lookup(ownerProcess.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	self, err := processidentity.Lookup(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := unix.Kqueue()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(kernel) })
	tracker := &tracker{
		kernel: kernel, owner: owner, self: self,
		watch: make(map[int]uint64), debt: make(map[uint64]time.Time), birth: make(map[uint64]time.Time),
	}
	if err := tracker.remember(owner); err != nil {
		t.Fatal(err)
	}
	if _, alive, err := tracker.scan(); err != nil || !alive {
		t.Fatal("initial scope", alive, err)
	}
	// The observer is outside the service scope and excluded from its snapshots.
	// Its independent child therefore has a missing outside birth edge.
	unrelated := exec.Command("/bin/sleep", "30")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
	live, alive, err := tracker.scan()
	if err != nil || !alive {
		t.Fatal("service stopped after unrelated activity", alive, err)
	}
	if tracker.unknown == 0 {
		t.Fatal("fixture did not produce missing host ancestry")
	}
	if tracker.issue != nil || len(tracker.debt) != 0 {
		t.Fatal("unrelated host ancestry poisoned the service scope", tracker.issue)
	}
	identity, err := processidentity.Lookup(unrelated.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, owned := tracker.known[identity.UniqueID]; owned {
		t.Fatal("unrelated process was adopted")
	}
	if _, owned := live[identity.UniqueID]; owned {
		t.Fatal("unrelated process became a cleanup target")
	}
}
