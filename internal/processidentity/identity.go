// Package processidentity identifies process lifetimes and immutable parent
// edges. Callers must obtain ownership roots from their own launch path, never
// from an input client, a process name or a persisted PID record from a prior
// service lifetime or boot.
package processidentity

import "errors"

var (
	ErrUnsupported = errors.New("generation-checked process control is unavailable")
	ErrChanged     = errors.New("process lifetime or exec generation changed")
	ErrInvalid     = errors.New("invalid process identity snapshot")
	ErrLimit       = errors.New("process identity snapshot exceeds its bound")
)

const maxProcesses = 65536

type Identity struct {
	PID            int    `json:"pid"`
	UniqueID       uint64 `json:"uniqueId"`
	ParentUniqueID uint64 `json:"parentUniqueId"`
	PIDVersion     uint32 `json:"pidVersion"`
	ParentPID      int    `json:"parentPid"`
	Group          int    `json:"group"`
	UID            uint32 `json:"uid"`
	State          uint32 `json:"state"`
}

func (i Identity) valid() bool { return i.PID > 1 && i.UniqueID != 0 && i.PIDVersion != 0 }

// SameLifetime intentionally ignores exec generation and the current PPID.
// Exec and reparenting do not transfer ownership to a different process.
func (i Identity) SameLifetime(other Identity) bool {
	return i.valid() && other.valid() && i.PID == other.PID && i.UniqueID == other.UniqueID
}

// Discover admits only immutable parent edges rooted in previously observed
// owned lifetimes. An absent intermediate is not inferred from names or groups.
func Discover(roots, snapshot []Identity) ([]Identity, error) {
	if len(roots) == 0 {
		return nil, ErrInvalid
	}
	if len(roots) > maxProcesses || len(snapshot) > maxProcesses {
		return nil, ErrLimit
	}
	owned := make(map[uint64]Identity, len(roots))
	for _, root := range roots {
		if !root.valid() {
			return nil, ErrInvalid
		}
		if previous, exists := owned[root.UniqueID]; exists && !root.SameLifetime(previous) {
			return nil, ErrInvalid
		}
		owned[root.UniqueID] = root
	}
	seen := make(map[uint64]Identity, len(snapshot))
	children := make(map[uint64][]Identity, len(snapshot))
	for _, value := range snapshot {
		if !value.valid() {
			return nil, ErrInvalid
		}
		if previous, exists := seen[value.UniqueID]; exists && (previous.PID != value.PID || previous.ParentUniqueID != value.ParentUniqueID) {
			return nil, ErrInvalid
		}
		if previous, exists := owned[value.UniqueID]; exists && !previous.SameLifetime(value) {
			return nil, ErrInvalid
		}
		if _, exists := seen[value.UniqueID]; exists {
			continue
		}
		seen[value.UniqueID] = value
		children[value.ParentUniqueID] = append(children[value.ParentUniqueID], value)
	}
	found := []Identity{}
	queue := make([]uint64, 0, len(roots)+len(snapshot))
	for _, root := range roots {
		queue = append(queue, root.UniqueID)
	}
	for index := 0; index < len(queue); index++ {
		for _, value := range children[queue[index]] {
			if _, exists := owned[value.UniqueID]; exists {
				continue
			}
			owned[value.UniqueID] = value
			found = append(found, value)
			queue = append(queue, value.UniqueID)
		}
	}
	return found, nil
}
