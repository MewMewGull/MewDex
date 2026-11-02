package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// FileChangePreview comes from an engine item event, including tools invoked
// through code mode. It never relies on parsing executable JavaScript or shell.
type FileChangePreview struct{ ThreadID, TurnID, ItemID, Patch string }

func FileChangePreviewFor(n Notification) (FileChangePreview, error) {
	var event struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Item     struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Status  string `json:"status"`
			Changes []struct {
				Path string `json:"path"`
				Kind struct {
					Type     string  `json:"type"`
					MovePath *string `json:"move_path"`
				} `json:"kind"`
				Diff string `json:"diff"`
			} `json:"changes"`
		} `json:"item"`
	}
	var preview FileChangePreview
	if json.Unmarshal(n.Params, &event) != nil {
		return preview, errors.New("文件变更事件格式无效")
	}
	preview.ThreadID, preview.TurnID, preview.ItemID = event.ThreadID, event.TurnID, event.Item.ID
	if n.Method != "item/started" || event.Item.Type != "fileChange" || event.Item.Status != "inProgress" || event.ThreadID == "" || event.TurnID == "" || event.Item.ID == "" {
		return preview, errors.New("文件变更未处于本回合的待执行状态")
	}
	if len(event.Item.Changes) == 0 || len(event.Item.Changes) > 64 {
		return preview, errors.New("文件变更为空或数量过多，请拆分请求")
	}
	sort.Slice(event.Item.Changes, func(i, j int) bool { return event.Item.Changes[i].Path < event.Item.Changes[j].Path })
	var output strings.Builder
	seen := map[string]bool{}
	appendLines := func(prefix, content string) {
		if content == "" {
			return
		}
		for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
			output.WriteString(prefix)
			output.WriteString(line)
			output.WriteByte('\n')
		}
	}
	for _, change := range event.Item.Changes {
		if change.Path == "" || strings.ContainsAny(change.Path, "\x00\r\n") || seen[change.Path] {
			return preview, errors.New("文件变更包含无效或重复路径")
		}
		seen[change.Path] = true
		if len(change.Diff) > 1<<20 || strings.ContainsRune(change.Diff, '\x00') {
			return preview, errors.New("文件变更过大或包含二进制内容，请拆分请求")
		}
		switch change.Kind.Type {
		case "add":
			fmt.Fprintf(&output, "--- /dev/null\n+++ %s\n", change.Path)
			appendLines("+", change.Diff)
		case "delete":
			fmt.Fprintf(&output, "--- %s\n+++ /dev/null\n", change.Path)
			appendLines("-", change.Diff)
		case "update":
			destination := change.Path
			if change.Kind.MovePath != nil {
				destination = *change.Kind.MovePath
				if destination == "" || strings.ContainsAny(destination, "\x00\r\n") {
					return preview, errors.New("文件移动目标无效")
				}
			}
			fmt.Fprintf(&output, "--- %s\n+++ %s\n", change.Path, destination)
			output.WriteString(change.Diff)
			if !strings.HasSuffix(change.Diff, "\n") {
				output.WriteByte('\n')
			}
		default:
			return preview, errors.New("文件变更包含不支持的操作")
		}
		if output.Len() > 1<<20 {
			return preview, errors.New("文件预览过大，请拆分请求")
		}
	}
	preview.Patch = output.String()
	return preview, nil
}
