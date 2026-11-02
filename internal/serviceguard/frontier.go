package serviceguard

import "mewdex/internal/processidentity"

// outsideFrontier classifies only observed immutable birth edges. Unknown
// ancestry is an uncertainty result, never permission to adopt or kill it.
type outsideFrontier struct {
	current map[uint64]bool
	started bool
}

func (f *outsideFrontier) observe(owned map[uint64]processidentity.Identity, snapshot []processidentity.Identity) (int, error) {
	roots := []processidentity.Identity{}
	outsideSnapshot := []processidentity.Identity{}
	for _, identity := range snapshot {
		if _, exists := owned[identity.UniqueID]; exists {
			continue
		}
		outsideSnapshot = append(outsideSnapshot, identity)
		if !f.started || f.current[identity.UniqueID] || f.current[identity.ParentUniqueID] || identity.ParentUniqueID == 1 {
			roots = append(roots, identity)
		}
	}
	outside := make(map[uint64]bool)
	if len(roots) > 0 {
		for _, identity := range roots {
			outside[identity.UniqueID] = true
		}
		children, err := processidentity.Discover(roots, outsideSnapshot)
		if err != nil {
			return 0, err
		}
		for _, identity := range children {
			outside[identity.UniqueID] = true
		}
	}
	unknown := 0
	for _, identity := range snapshot {
		if _, exists := owned[identity.UniqueID]; !exists && !outside[identity.UniqueID] {
			unknown++
		}
	}
	// Bound host history to its current frontier. A late missing birth edge must
	// remain uncertain instead of accumulating indefinite host process history.
	f.current, f.started = outside, true
	return unknown, nil
}
