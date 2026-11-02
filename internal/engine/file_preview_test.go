package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeFilePreviewPreservesChangesAndRejectsAmbiguity(t *testing.T) {
	for _, test := range []struct {
		kind, path, diff string
		valid            bool
	}{
		{"add", "project/a.txt", "new line\n", true}, {"delete", "project/a.txt", "old line\n", true}, {"update", "project/a.txt", "@@ -1 +1 @@\n-old\n+new\n", true}, {"unknown", "project/a.txt", "text", false}, {"add", "project/a.txt\nother.txt", "text", false}, {"add", "project/a.txt", "binary\x00", false},
	} {
		raw, _ := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"type": "fileChange", "id": "inner-code-mode-call", "status": "inProgress", "changes": []any{map[string]any{"path": test.path, "kind": map[string]string{"type": test.kind}, "diff": test.diff}}}})
		preview, err := FileChangePreviewFor(Notification{Method: "item/started", Params: raw})
		if (err == nil) != test.valid {
			t.Fatalf("%s %v", test.kind, err)
		}
		if test.valid && (preview.ThreadID != "thread" || preview.TurnID != "turn" || preview.ItemID != "inner-code-mode-call" || !strings.Contains(preview.Patch, test.path)) {
			t.Fatal("preview lost its native binding", preview)
		}
	}
}
