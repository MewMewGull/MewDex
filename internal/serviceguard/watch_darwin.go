//go:build darwin

package serviceguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"mewdex/internal/lifecycle"
	"mewdex/internal/processidentity"
)

type tracker struct {
	lifetimeHistory
	kernel               int
	owner                processidentity.Identity
	self                 processidentity.Identity
	watch                map[int]uint64
	debt                 map[uint64]time.Time
	birth                map[uint64]time.Time
	issue                error
	credentialTransition bool
	frontier             outsideFrontier
	unknown              int
}

type cleanupReport struct {
	ID                   string                     `json:"id"`
	OwnerPID             int                        `json:"ownerPid"`
	WatcherPID           int                        `json:"watcherPid"`
	ObservedLifetimes    uint64                     `json:"observedLifetimes"`
	RetainedLifetimes    int                        `json:"retainedLifetimes"`
	RetiredLifetimes     uint64                     `json:"retiredLifetimes"`
	PeakTrackedLifetimes int                        `json:"peakTrackedLifetimes"`
	RemainingObserved    int                        `json:"remainingObserved"`
	UnresolvedForks      int                        `json:"unresolvedForks"`
	ObservedCleanup      bool                       `json:"observedCleanupConfirmed"`
	Coverage             string                     `json:"coverage"`
	UnobservedAncestors  bool                       `json:"unobservedIntermediateAncestorsPossible"`
	Error                string                     `json:"error,omitempty"`
	Identities           []processidentity.Identity `json:"identities,omitempty"`
	CredentialTransition bool                       `json:"observedOwnedCredentialTransition"`
	UnresolvedAncestry   int                        `json:"unresolvedNewAncestry"`
}

func (t *tracker) remember(identity processidentity.Identity) error {
	if identity.SameLifetime(t.self) {
		return nil
	}
	added, err := t.lifetimeHistory.remember(identity, trackingLimit)
	if err != nil {
		return err
	}
	if added {
		t.birth[identity.ParentUniqueID] = time.Now()
	}
	if identity.UID != t.self.UID {
		t.credentialTransition = true
	}
	if identity.State == 5 || t.watch[identity.PID] == identity.UniqueID {
		return nil
	}
	// An earlier filter for a reused PID must not observe the new lifetime as
	// the old child. Kernel signals independently validate the exec generation.
	if _, exists := t.watch[identity.PID]; exists {
		old := unix.Kevent_t{Ident: uint64(identity.PID), Filter: unix.EVFILT_PROC, Flags: unix.EV_DELETE}
		_, _ = unix.Kevent(t.kernel, []unix.Kevent_t{old}, nil, nil)
	}
	event := unix.Kevent_t{Ident: uint64(identity.PID), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_CLEAR, Fflags: unix.NOTE_FORK | unix.NOTE_EXEC | unix.NOTE_EXIT}
	_, err = unix.Kevent(t.kernel, []unix.Kevent_t{event}, nil, nil)
	if errors.Is(err, syscall.ESRCH) {
		return nil // Keep the immutable edge even if this process just exited.
	}
	if err == nil {
		t.watch[identity.PID] = identity.UniqueID
	}
	return err
}

func (t *tracker) scan() (map[uint64]processidentity.Identity, bool, error) {
	pids, err := processidentity.ListPIDs()
	if err != nil {
		return nil, false, err
	}
	snapshot := make([]processidentity.Identity, 0, len(pids))
	for _, pid := range pids {
		identity, err := processidentity.Lookup(pid)
		if err != nil {
			if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ENOENT) {
				continue
			}
			if _, known := t.watch[pid]; known || !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES) {
				return nil, false, err
			}
			continue // A process outside this user's observable scope is not adopted.
		}
		if !identity.SameLifetime(t.self) {
			snapshot = append(snapshot, identity)
		}
	}
	roots := make([]processidentity.Identity, 0, len(t.known))
	for _, identity := range t.known {
		roots = append(roots, identity)
	}
	found, err := processidentity.Discover(roots, snapshot)
	if err != nil {
		return nil, false, err
	}
	for _, identity := range found {
		delete(t.debt, identity.ParentUniqueID)
		if err = t.remember(identity); err != nil {
			return nil, false, err
		}
	}
	// Missing ancestry elsewhere on the host is diagnostic information, not
	// evidence of a fork by this service. A short-lived unrelated parent can
	// disappear between snapshots while its child remains. Only unresolved fork
	// events from the tracked service lifetimes can make our own scope uncertain.
	t.unknown, err = t.frontier.observe(t.known, snapshot)
	if err != nil {
		return nil, false, err
	}
	live := make(map[uint64]processidentity.Identity)
	ownerAlive := false
	for _, identity := range snapshot {
		if previous, owned := t.known[identity.UniqueID]; !owned || !previous.SameLifetime(identity) {
			continue
		}
		if err = t.remember(identity); err != nil {
			return nil, false, err
		}
		if identity.State == 5 { // Zombies no longer execute or hold user file leases.
			continue
		}
		if identity.SameLifetime(t.owner) {
			ownerAlive = true
		} else {
			live[identity.UniqueID] = identity
		}
	}
	// Only retire identities whose kernel filter is gone, which stayed absent
	// from complete snapshots and have no unresolved/recent birth evidence.
	// Already observed live children remain independent ownership roots.
	now := time.Now()
	protected := map[uint64]bool{t.owner.UniqueID: true}
	for _, id := range t.watch {
		protected[id] = true
	}
	for id := range t.debt {
		protected[id] = true
	}
	for id, at := range t.birth {
		if now.Sub(at) < exitedHistoryGrace {
			protected[id] = true
		}
	}
	for _, id := range t.lifetimeHistory.trim(now, snapshot, protected) {
		delete(t.birth, id)
	}
	return live, ownerAlive, nil
}

func (t *tracker) events(wait time.Duration) error {
	var events [64]unix.Kevent_t
	timeout := unix.NsecToTimespec(int64(wait))
	n, err := unix.Kevent(t.kernel, nil, events[:], &timeout)
	if errors.Is(err, syscall.EINTR) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, event := range events[:n] {
		if event.Flags&unix.EV_ERROR != 0 && event.Data != 0 {
			return syscall.Errno(event.Data)
		}
		if event.Fflags&unix.NOTE_FORK != 0 {
			if id, exists := t.watch[int(event.Ident)]; exists {
				// The snapshot and kevent delivery can race in either order.
				// A recently captured birth edge accounts for that delayed event.
				if time.Since(t.birth[id]) > 100*time.Millisecond {
					t.debt[id] = time.Now()
				}
			}
		}
		if event.Fflags&unix.NOTE_EXIT != 0 {
			delete(t.watch, int(event.Ident))
		}
	}
	return nil
}

func (t *tracker) own(expected processidentity.Identity) error {
	if err := t.events(0); err != nil {
		return err
	}
	current, err := processidentity.Lookup(expected.PID)
	if err != nil {
		return err
	}
	if !expected.SameLifetime(current) || expected.PIDVersion != current.PIDVersion || current.UID != t.self.UID {
		return processidentity.ErrChanged
	}
	if _, owned := t.known[current.ParentUniqueID]; !owned || current.SameLifetime(t.self) {
		return errors.New("guarded child is not descended from this service lifetime")
	}
	delete(t.debt, current.ParentUniqueID)
	return t.remember(current)
}

func (t *tracker) cleanup(start startMessage) error {
	report := cleanupReport{ID: start.ID, OwnerPID: t.owner.PID, WatcherPID: t.self.PID, Coverage: "observed immutable birth edges", UnobservedAncestors: true}
	stable := 0
	remaining := 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := t.events(10 * time.Millisecond); err != nil {
			if t.issue == nil {
				t.issue = err
			}
		}
		live, _, err := t.scan()
		if err != nil {
			if t.issue == nil {
				t.issue = err
			}
			break
		}
		remaining = len(live)
		if remaining == 0 {
			stable++
			if stable >= 3 {
				break
			}
		} else {
			stable = 0
		}
		for _, identity := range live {
			if err = processidentity.Signal(identity, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, processidentity.ErrChanged) {
				if t.issue == nil {
					t.issue = err
				}
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	report.ObservedLifetimes, report.RemainingObserved, report.UnresolvedForks = t.total-1, remaining, len(t.debt)
	report.RetainedLifetimes, report.RetiredLifetimes, report.PeakTrackedLifetimes = len(t.known)-1, t.retired, t.peak-1
	report.CredentialTransition = t.credentialTransition
	report.UnresolvedAncestry = t.unknown
	if stable < 3 || remaining != 0 || len(t.debt) != 0 {
		t.issue = errors.Join(t.issue, lifecycle.ErrUnconfirmed)
	}
	report.ObservedCleanup = t.issue == nil
	if t.issue != nil {
		report.Error = t.issue.Error()
		for _, identity := range t.known {
			if len(report.Identities) >= 256 {
				break
			}
			report.Identities = append(report.Identities, identity)
		}
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(start.Directories[0], "service-ownership.report.json"), append(raw, '\n'), 0600)
	}
	if err != nil || t.issue != nil {
		return errors.Join(t.issue, err) // Keep refusal markers on every uncertain path.
	}
	return lifecycle.CompleteOwnership(start.Directories, start.ID)
}

func watch() error {
	input, output := os.NewFile(3, "owner-input"), os.NewFile(4, "owner-output")
	defer input.Close()
	defer output.Close()
	syscall.CloseOnExec(3)
	syscall.CloseOnExec(4)
	_ = input.SetReadDeadline(time.Now().Add(8 * time.Second))
	read := scanner(input)
	var start startMessage
	if !read.Scan() || json.Unmarshal(read.Bytes(), &start) != nil || len(start.ID) < 16 || len(start.Directories) < 1 || len(start.Directories) > 2 {
		return errors.New("invalid inherited process ownership configuration")
	}
	_ = input.SetReadDeadline(time.Time{})
	for index, directory := range start.Directories {
		file := os.NewFile(uintptr(5+index), "inherited-service-lease")
		defer file.Close()
		syscall.CloseOnExec(5 + index)
		actual, err := file.Stat()
		expected, pathErr := os.Stat(filepath.Join(directory, "service.lock"))
		if err != nil || pathErr != nil || !actual.Mode().IsRegular() || !os.SameFile(actual, expected) {
			return errors.New("inherited service lease differs from its owned directory")
		}
	}
	current, err := processidentity.Lookup(os.Getppid())
	if err != nil || !current.SameLifetime(start.Owner) || start.Owner.UID != uint32(os.Geteuid()) {
		return errors.New("process monitor parent lifetime changed before handoff")
	}
	if err = processidentity.Available(); err != nil {
		return err
	}
	self, err := processidentity.Lookup(os.Getpid())
	if err != nil {
		return err
	}
	kernel, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(kernel)
	unix.CloseOnExec(kernel)
	t := &tracker{kernel: kernel, owner: current, self: self, watch: make(map[int]uint64), debt: make(map[uint64]time.Time), birth: make(map[uint64]time.Time)}
	if err = t.remember(current); err != nil {
		return err
	}
	if _, _, err = t.scan(); err != nil {
		return err
	}
	if err = send(output, reply{State: "ready"}); err != nil {
		return errors.Join(err, t.cleanup(start))
	}
	requests := make(chan request, 16)
	readDone := make(chan struct{})
	go func() {
		defer close(requests)
		for read.Scan() {
			var message request
			if json.Unmarshal(read.Bytes(), &message) != nil {
				return
			}
			select {
			case requests <- message:
			case <-readDone:
				return
			}
		}
	}()
	defer close(readDone)
	finishRequested := false
	for !finishRequested {
		select {
		case message, open := <-requests:
			if !open || message.Operation == "finish" {
				finishRequested = true
				break
			}
			if message.Operation != "own" {
				t.issue = errors.New("invalid process ownership request")
				finishRequested = true
				break
			}
			if err = t.own(message.Identity); err != nil {
				t.issue = err
				_ = send(output, reply{State: "failed", Error: err.Error()})
				finishRequested = true
				break
			}
			if err = send(output, reply{State: "owned"}); err != nil {
				finishRequested = true
			}
		default:
		}
		if finishRequested {
			break
		}
		if err = t.events(25 * time.Millisecond); err != nil {
			t.issue = err
			break
		}
		_, alive, err := t.scan()
		if err != nil {
			t.issue = err
			break
		}
		if !alive {
			break
		}
		if t.issue != nil {
			break
		}
		// A missing observed fork edge is not silently declared a clean scope.
		// Debt is retained until an immutable new child edge is actually found.
		for _, since := range t.debt {
			if time.Since(since) > 500*time.Millisecond {
				t.issue = errors.Join(lifecycle.ErrUnconfirmed, errors.New("a process fork was observed without its immutable child edge"))
				finishRequested = true
				break
			}
		}
	}
	err = t.cleanup(start)
	result := reply{State: "stopped"}
	if err != nil {
		result.Error = fmt.Sprint(err)
	}
	if writeErr := send(output, result); writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) && !errors.Is(writeErr, syscall.EPIPE) {
		err = errors.Join(err, writeErr)
	}
	return err
}
