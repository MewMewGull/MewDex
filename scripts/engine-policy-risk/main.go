// Development probe for pinned engine policy semantics; never imports account credentials.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	c, err := config.Load(".local/device.json")
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp(".state", "engine-policy-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	home := filepath.Join(root, "engine-home")
	workspace := filepath.Join(root, "workspace")
	if err = os.MkdirAll(workspace, 0700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	e, err := engine.Start(ctx, config.Engine{Executable: c.Engine.Executable, Args: c.Engine.Args, Home: home})
	if err != nil {
		return err
	}
	defer e.Close()
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if err = e.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		return err
	}
	if string(account.Account) != "null" && len(account.Account) > 0 {
		return errors.New("isolated probe unexpectedly has an account; no turn submitted")
	}
	var reply struct {
		Model  string `json:"model"`
		Thread struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"thread"`
	}
	params := map[string]any{"cwd": workspace, "baseInstructions": "MEWDEX_BASE_ONE", "developerInstructions": "MEWDEX_DEVELOPER_ONE", "approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": "read-only"}
	if err = e.Call(ctx, "thread/start", params, &reply); err != nil {
		return err
	}
	thread := reply.Thread.ID
	lastTurn := ""
	turnInstructions := ""
	turn := func() error {
		var data struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		params := map[string]any{"threadId": thread, "input": []map[string]string{{"type": "text", "text": "Policy compatibility probe. No tools or inference authorization is provided."}}}
		if turnInstructions != "" {
			params["collaborationMode"] = map[string]any{"mode": "default", "settings": map[string]any{"model": reply.Model, "developer_instructions": turnInstructions}}
		}
		if err := e.Call(ctx, "turn/start", params, &data); err != nil {
			return err
		}
		lastTurn = data.Turn.ID
		for {
			select {
			case n, ok := <-e.Notifications():
				if !ok {
					return errors.New("probe engine disconnected")
				}
				if len(n.RequestID) > 0 {
					_ = e.Reject(ctx, n.RequestID, "No tools authorized in this probe")
				}
				var notification struct {
					ThreadID string `json:"threadId"`
					Turn     struct {
						ID string `json:"id"`
					} `json:"turn"`
				}
				_ = json.Unmarshal(n.Params, &notification)
				if n.Method == "turn/completed" && notification.ThreadID == thread && notification.Turn.ID == lastTurn {
					return nil
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if err = turn(); err != nil {
		return err
	}
	read := func() (map[string]bool, error) {
		if err := e.Call(ctx, "thread/read", map[string]any{"threadId": thread, "includeTurns": false}, &reply); err != nil {
			return nil, err
		}
		path := reply.Thread.Path
		relative, err := filepath.Rel(home, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, errors.New("probe rollout is outside isolated home")
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, readErr
			}
			complete := false
			for _, line := range strings.Split(string(body), "\n") {
				var record struct {
					Type    string `json:"type"`
					Payload struct {
						Type   string `json:"type"`
						TurnID string `json:"turn_id"`
					} `json:"payload"`
				}
				if json.Unmarshal([]byte(line), &record) == nil && record.Type == "event_msg" && record.Payload.Type == "task_complete" && record.Payload.TurnID == lastTurn {
					complete = true
				}
			}
			if complete {
				break
			}
			if time.Now().After(deadline) {
				return nil, errors.New("probe rollout has not flushed its latest turn")
			}
			time.Sleep(100 * time.Millisecond)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		markers := map[string]bool{"baseOne": false, "baseTwo": false, "developerOne": false, "developerTwo": false, "effectiveBaseOne": false, "effectiveBaseTwo": false}
		scan := bufio.NewScanner(file)
		scan.Buffer(make([]byte, 65536), 2*1024*1024)
		for scan.Scan() {
			line := scan.Text()
			var record struct {
				Type    string `json:"type"`
				Payload struct {
					Base json.RawMessage `json:"base_instructions"`
				} `json:"payload"`
			}
			if json.Unmarshal(scan.Bytes(), &record) == nil && record.Type == "session_meta" {
				markers["effectiveBaseOne"] = strings.Contains(string(record.Payload.Base), "MEWDEX_BASE_ONE")
				markers["effectiveBaseTwo"] = strings.Contains(string(record.Payload.Base), "MEWDEX_BASE_TWO")
			}
			for key, marker := range map[string]string{"baseOne": "MEWDEX_BASE_ONE", "baseTwo": "MEWDEX_BASE_TWO", "developerOne": "MEWDEX_DEVELOPER_ONE", "developerTwo": "MEWDEX_DEVELOPER_TWO"} {
				if strings.Contains(line, marker) {
					markers[key] = true
				}
			}
		}
		return markers, scan.Err()
	}
	first, err := read()
	if err != nil {
		return err
	}
	if err = e.Call(ctx, "thread/resume", map[string]any{"threadId": thread, "baseInstructions": "MEWDEX_BASE_TWO", "developerInstructions": "MEWDEX_DEVELOPER_TWO", "approvalsReviewer": "user", "excludeTurns": true}, &reply); err != nil {
		return err
	}
	if err = turn(); err != nil {
		return err
	}
	second, err := read()
	if err != nil {
		return err
	}
	turnInstructions = "MEWDEX_DEVELOPER_TWO"
	if err = turn(); err != nil {
		return err
	}
	perTurn, err := read()
	if err != nil {
		return err
	}
	if err = e.Call(ctx, "thread/fork", map[string]any{"threadId": thread, "baseInstructions": "MEWDEX_BASE_TWO", "developerInstructions": "MEWDEX_DEVELOPER_TWO", "approvalsReviewer": "user", "excludeTurns": true}, &reply); err != nil {
		return err
	}
	thread = reply.Thread.ID
	if err = turn(); err != nil {
		return err
	}
	third, err := read()
	if err != nil {
		return err
	}
	if err = e.Call(ctx, "thread/fork", map[string]any{"threadId": thread, "developerInstructions": "MEWDEX_DEFAULT_RESTORED", "approvalsReviewer": "user", "excludeTurns": true}, &reply); err != nil {
		return err
	}
	thread = reply.Thread.ID
	if err = turn(); err != nil {
		return err
	}
	restored, err := read()
	if err != nil {
		return err
	}
	report := map[string]any{"at": protocol.Now(), "probeDirectory": root, "isolatedAccount": true, "accountAuthorized": false, "firstTurnRollout": first, "resumedLoadedThreadRollout": second, "perTurnInstructionsRollout": perTurn, "forkedThreadRollout": third, "restoredDefaultRollout": restored}
	if err = os.WriteFile(".state/evidence/engine-policy-semantics.json", protocol.JSON(report), 0600); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
