package engine

// CanReadHistoryVersion is deliberately an exact compatibility list. Reading an
// owned 0.154.0 rollout in a fresh 0.162.1 home preserves its thread, turn, and
// client receipt IDs. Unknown or newer formats require a separate migration.
func CanReadHistoryVersion(version string) bool {
	return version == CompatibleVersion || (CompatibleVersion == "0.162.1" && version == "0.154.0")
}
