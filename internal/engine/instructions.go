package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// BaseInstructionReader is optional for engines that expose their own persisted
// instruction snapshot. It does not accept a path supplied by an input client.
type BaseInstructionReader interface {
	ReadBaseInstructions(context.Context, string) (string, error)
}

func (e *Codex) openThreadHistory(ctx context.Context, threadID string) (*os.File, error) {
	var reply struct {
		Thread struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"thread"`
	}
	if err := e.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &reply); err != nil {
		return nil, err
	}
	if reply.Thread.ID != threadID {
		return nil, errors.New("engine returned another thread's history")
	}
	root, err := filepath.EvalSymlinks(e.home)
	if err != nil {
		return nil, err
	}
	path, err := filepath.EvalSymlinks(reply.Thread.Path)
	if err != nil {
		return nil, err
	}
	root, path = comparableHistoryPath(root), comparableHistoryPath(path)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("history snapshot is outside the engine data directory")
	}
	bounded, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer bounded.Close()
	file, err := bounded.Open(relative)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("engine history is not a regular file")
	}
	return file, nil
}

// Native Windows engines can return an extended-length path for the same
// owned file. Normalize only DOS/UNC spelling, never device namespaces; the
// subsequent relative check and os.Root open still enforce the home boundary.
func comparableHistoryPath(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	if len(path) >= 8 && strings.EqualFold(path[:8], `\\?\UNC\`) {
		return `\\` + path[8:]
	}
	if strings.HasPrefix(path, `\\?\`) {
		ordinary := path[4:]
		if len(ordinary) >= 3 && ordinary[1] == ':' && filepath.IsAbs(ordinary) {
			return ordinary
		}
	}
	return path
}

func (e *Codex) ReadBaseInstructions(ctx context.Context, threadID string) (string, error) {
	file, err := e.openThreadHistory(ctx, threadID)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 65536), 2*1024*1024)
	if scanner.Scan() {
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				ID   string          `json:"id"`
				Base json.RawMessage `json:"base_instructions"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) == nil && record.Type == "session_meta" && record.Payload.ID == threadID {
			var text string
			if json.Unmarshal(record.Payload.Base, &text) == nil {
				return text, nil
			}
			var base struct {
				Text *string `json:"text"`
			}
			if json.Unmarshal(record.Payload.Base, &base) == nil && base.Text != nil {
				return *base.Text, nil
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("engine instruction snapshot has not been persisted")
}
