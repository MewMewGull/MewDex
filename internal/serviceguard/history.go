package serviceguard

import (
	"time"

	"mewdex/internal/processidentity"
)

const trackingLimit = 65536
const exitedHistoryGrace = 2 * time.Second

// lifetimeHistory bounds retained ancestry rather than the total number of
// tools a service may ever execute. The native observer supplies protected
// identities for roots, live kernel filters and unresolved fork evidence.
type lifetimeHistory struct {
	known   map[uint64]processidentity.Identity
	absent  map[uint64]time.Time
	total   uint64
	retired uint64
	peak    int
}

func (h *lifetimeHistory) remember(identity processidentity.Identity, limit int) (bool, error) {
	if !identity.SameLifetime(identity) {
		return false, processidentity.ErrInvalid
	}
	previous, exists := h.known[identity.UniqueID]
	if exists && !previous.SameLifetime(identity) {
		return false, processidentity.ErrInvalid
	}
	if !exists && len(h.known) >= limit {
		return false, processidentity.ErrLimit
	}
	if h.known == nil {
		h.known = make(map[uint64]processidentity.Identity)
	}
	h.known[identity.UniqueID] = identity
	if !exists {
		h.total++
		if len(h.known) > h.peak {
			h.peak = len(h.known)
		}
	}
	return !exists, nil
}

// trim is called only after a complete native snapshot has already discovered
// descendants and classified unknown ancestry. A live process is never aged
// out; an exit without a still-active filter must remain absent through the
// grace interval. Unknown forks and recent births are protected by the caller.
func (h *lifetimeHistory) trim(now time.Time, snapshot []processidentity.Identity, protected map[uint64]bool) []uint64 {
	present := make(map[uint64]bool, len(snapshot))
	for _, identity := range snapshot {
		present[identity.UniqueID] = true
	}
	if h.absent == nil {
		h.absent = make(map[uint64]time.Time)
	}
	retired := []uint64{}
	for id := range h.known {
		if present[id] || protected[id] {
			delete(h.absent, id)
			continue
		}
		since, waiting := h.absent[id]
		if !waiting {
			h.absent[id] = now
			continue
		}
		if now.Sub(since) < exitedHistoryGrace {
			continue
		}
		delete(h.known, id)
		delete(h.absent, id)
		h.retired++
		retired = append(retired, id)
	}
	return retired
}
