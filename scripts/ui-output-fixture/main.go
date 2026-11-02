// Append labelled display-only output to the existing frozen UI conversation.
// The Hub must be stopped; this never creates an executor or invokes an engine.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func run() error {
	data := flag.String("data", ".local/data", "stopped development Hub data directory")
	inspect := flag.Bool("inspect", false, "read the fixture fields without appending display events")
	flag.Parse()
	lock, err := lifecycle.Acquire(*data)
	if err != nil {
		return err
	}
	defer lock.Close()
	s, err := store.Open(filepath.Join(*data, "hub.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	var session protocol.Session
	if err = s.Get("sessions", "mewdex-ui-history-20261028", &session); err != nil {
		return err
	}
	if *inspect {
		fmt.Println(string(protocol.JSON(map[string]any{"id": session.ID, "deviceId": session.DeviceID, "title": session.Title, "engineId": session.EngineID, "engineStarted": session.EngineStarted, "frozen": session.Frozen, "state": session.State})))
		return nil
	}
	// The inert fixture can retain EngineStarted from an earlier refused call.
	// That historical flag is preserved; only display events are appended here.
	if session.DeviceID != "mewdex-ui-fixture-20261028" || session.Title != "界面检查：历史与冻结队列（模拟记录）" || session.EngineID != "" || !session.Frozen || session.State != "paused" {
		return errors.New("only the existing frozen, labelled UI fixture can receive display data")
	}
	var output strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&output, "[模拟输出 %02d] Windows 输出面板的阅读样例；未运行构建。\r\n", i)
	}
	output.WriteString("\x1b[32m[模拟结果] 显示数据已准备\x1b[0m\r\n")
	diff := "diff --git a/demo/example.ts b/demo/example.ts\n--- a/demo/example.ts\n+++ b/demo/example.ts\n@@ -1,2 +1,3 @@\n-const label = 'before';\n+const label = '模拟差异';\n+const visible = true;\n export { label };\ndiff --git \"a/demo/说明.txt\" \"b/demo/说明.txt\"\nnew file mode 100644\n--- /dev/null\n+++ \"b/demo/说明.txt\"\n@@ -0,0 +1,2 @@\n+这是输出面板的模拟文件。\n+没有实际修改这些项目文件。\n"
	items := []struct {
		id, kind string
		payload  any
	}{
		{"command-build", "engine.item/completed", map[string]any{"item": map[string]any{"id": "ui-output-build-20261029", "type": "commandExecution", "command": "preview-build（模拟命令，未执行）", "cwd": "D:\\MewDexPreview", "status": "completed", "aggregatedOutput": output.String(), "exitCode": 0, "durationMs": 1250}}},
		{"command-check", "engine.item/completed", map[string]any{"item": map[string]any{"id": "ui-output-check-20261029", "type": "commandExecution", "command": "preview-check（模拟失败，未执行）", "cwd": "D:\\MewDexPreview", "status": "failed", "aggregatedOutput": "[模拟错误] 一条检查信息，用于显示退出码与失败样式。\r\n", "exitCode": 2, "durationMs": 230}}},
		{"diff", "engine.turn/diff/updated", map[string]string{"turnId": "ui-output-preview-20261029", "diff": diff}},
	}
	for _, item := range items {
		_, err = s.Append(protocol.Event{ID: "ui-output-reading-20261029:" + item.id, DeviceID: session.DeviceID, SessionID: session.ID, Kind: item.kind, Payload: protocol.JSON(item.payload), CreatedAt: protocol.Now()})
		if err != nil {
			return err
		}
	}
	fmt.Println(`{"synthetic":true,"sessionId":"mewdex-ui-history-20261028","displayEventIds":3,"modelCalls":0,"commandsExecuted":0,"sessionAndQueueUnchanged":true}`)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
