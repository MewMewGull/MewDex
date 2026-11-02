package policy

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"mewdex/internal/protocol"
)

// ConfigSources only copies instruction fields, never complete configuration maps.
func ConfigSources(raw json.RawMessage) ([]protocol.PromptSource, error) {
	var reply struct {
		Layers []struct {
			Name struct {
				Type   string `json:"type"`
				File   string `json:"file"`
				Folder string `json:"dotCodexFolder"`
			} `json:"name"`
			Config         map[string]json.RawMessage `json:"config"`
			DisabledReason *string                    `json:"disabledReason"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, err
	}
	out := []protocol.PromptSource{}
	for _, layer := range reply.Layers {
		path := layer.Name.File
		if path == "" {
			path = layer.Name.Folder
		}
		for _, key := range []string{"instructions", "developer_instructions"} {
			var value string
			if json.Unmarshal(layer.Config[key], &value) != nil || value == "" {
				continue
			}
			note := ""
			if layer.DisabledReason != nil {
				note = "执行端未启用此配置层"
			}
			out = append(out, protocol.PromptSource{ID: layer.Name.Type + ":" + path + ":" + key, Kind: "engine-config", Label: layer.Name.Type + " · " + key, Path: path, Text: value, ReadOnly: true, Enabled: layer.DisabledReason == nil, Note: note})
		}
	}
	return out, nil
}

func ProjectSources(root string) []protocol.PromptSource {
	out := []protocol.PromptSource{}
	for _, name := range []string{"AGENTS.override.md", "AGENTS.md"} {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		source := protocol.PromptSource{ID: "project:" + name, Kind: "project", Label: name, Path: path, ReadOnly: true, Enabled: true}
		real, err := filepath.EvalSymlinks(path)
		if err == nil {
			var relative string
			relative, err = filepath.Rel(root, real)
			if err == nil && (relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
				source.Note = "来源位于配置的项目目录之外，未展开内容"
				out = append(out, source)
				break
			}
		}
		if err != nil {
			source.Note = "无法读取项目指令来源"
			out = append(out, source)
			break
		}
		file, err := os.Open(real)
		if err == nil {
			var data []byte
			data, err = io.ReadAll(io.LimitReader(file, 256*1024+1))
			_ = file.Close()
			if err == nil {
				if len(data) > 256*1024 {
					source.Note = "内容超过 256 KiB，未展开"
				} else if !utf8.Valid(data) {
					source.Note = "内容不是有效 UTF-8"
				} else {
					source.Text = string(data)
				}
			}
		}
		if err != nil {
			source.Note = "无法读取项目指令来源"
		}
		out = append(out, source)
		// Codex selects override before AGENTS in the same directory.
		break
	}
	return out
}
