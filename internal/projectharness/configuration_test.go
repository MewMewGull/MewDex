package projectharness

import (
	"encoding/json"
	"strings"
	"testing"

	"mewdex/internal/protocol"
)

func TestProjectConfigurationKeepsLiteralArgvAndDurableBindings(t *testing.T) {
	configuration := Empty()
	configuration.Skills = []protocol.ProjectSkill{{ID: "review", Name: "代码检查", Instructions: "保留原有内容。"}}
	configuration.Scripts = []protocol.ProjectScript{{ID: "check", Name: "检查", DeviceID: "node", ProjectID: "work", Command: []string{"tool", "", "目录 空格", "$(literal)", "line\nbreak"}, Cwd: "src/../tests"}}
	configuration.Workflows = []protocol.ProjectWorkflow{{ID: "verify", Name: "验证", Steps: []string{"check", "check"}}}
	directories := []protocol.ProjectDirectory{{DeviceID: "node", ProjectID: "work"}}
	normalized, err := Normalize(configuration, directories)
	if err != nil || string(protocol.JSON(configuration)) != string(protocol.JSON(normalized)) {
		t.Fatal("configuration rewrote literal content", err)
	}
	for _, change := range []string{"format", "foreign-node", "duplicate", "cwd", "nul", "workflow", "oversized"} {
		t.Run(change, func(t *testing.T) {
			var invalid protocol.ProjectHarness
			_ = json.Unmarshal(protocol.JSON(configuration), &invalid)
			switch change {
			case "format":
				invalid.Format++
			case "foreign-node":
				invalid.Scripts[0].DeviceID = "other"
			case "duplicate":
				invalid.Skills = append(invalid.Skills, invalid.Skills[0])
			case "cwd":
				invalid.Scripts[0].Cwd = "src/../../outside"
			case "nul":
				invalid.Scripts[0].Command = append(invalid.Scripts[0].Command, "bad\x00value")
			case "workflow":
				invalid.Workflows[0].Steps = []string{"undefined"}
			case "oversized":
				invalid.Instructions = strings.Repeat("x", 64<<10+1)
			}
			if _, err = Normalize(invalid, directories); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}
