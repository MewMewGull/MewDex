package engine

import (
	"path"
	"strings"
)

// IsHistoryBackupFile selects pinned-engine conversation rollouts, never home
// configuration, authentication stores or keychain material.
func IsHistoryBackupFile(relative string) bool {
	if !strings.HasPrefix(relative, "sessions/") && !strings.HasPrefix(relative, "archived_sessions/") {
		return false
	}
	base := path.Base(relative)
	return strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl")
}
