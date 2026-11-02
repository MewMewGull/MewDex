package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

type filePreviewEntry struct {
	SessionID, ScopeID string
	Preview            engine.FileChangePreview
	Error              string
}

func filePreviewKey(thread, turn, item string) string {
	digest := sha256.Sum256(protocol.JSON([]string{thread, turn, item}))
	return hex.EncodeToString(digest[:])
}

// The session lock orders item events and approvals. This bounded cache only
// serves the current process; a restart expires its callbacks and previews.
func (x *Executor) captureFilePreview(s protocol.Session, n engine.Notification) {
	if n.Method != "item/started" && n.Method != "item/completed" {
		return
	}
	var item struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Item     struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"item"`
	}
	if json.Unmarshal(n.Params, &item) != nil || item.ThreadID != s.EngineID || item.TurnID == "" || item.TurnID != s.TurnID || item.Item.Type != "fileChange" || item.Item.ID == "" {
		return
	}
	key := filePreviewKey(item.ThreadID, item.TurnID, item.Item.ID)
	x.mu.Lock()
	defer x.mu.Unlock()
	if n.Method == "item/completed" {
		delete(x.filePreviews, key)
		return
	}
	if _, exists := x.filePreviews[key]; !exists && len(x.filePreviews) >= 64 {
		return
	}
	preview, err := engine.FileChangePreviewFor(n)
	entry := filePreviewEntry{SessionID: s.ID, ScopeID: s.ExecutionScopeID, Preview: preview}
	if err != nil {
		entry.Error = err.Error()
	}
	x.filePreviews[key] = entry
}
