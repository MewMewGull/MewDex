// Package projectharness validates the versioned, transport-neutral project
// configuration. Execution and account permissions stay with the existing runner.
package projectharness

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"mewdex/internal/protocol"
)

const Format = 1

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func Empty() protocol.ProjectHarness {
	return protocol.ProjectHarness{Format: Format, Skills: []protocol.ProjectSkill{}, Scripts: []protocol.ProjectScript{}, Workflows: []protocol.ProjectWorkflow{}}
}

func Version(configuration protocol.ProjectHarness) string {
	digest := sha256.Sum256(protocol.JSON(configuration))
	return hex.EncodeToString(digest[:])
}

func metadata(id, name, description string, seen map[string]bool) error {
	if !identifier.MatchString(id) || seen[id] || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 100 || len(description) > 4096 {
		return errors.New("条目需要唯一的小写英文编号、名称和有效说明")
	}
	seen[id] = true
	return nil
}

func Normalize(input protocol.ProjectHarness, directories []protocol.ProjectDirectory) (protocol.ProjectHarness, error) {
	if input.Format != Format || len(input.Instructions) > 64<<10 || len(input.Skills) > 64 || len(input.Scripts) > 64 || len(input.Workflows) > 32 || len(protocol.JSON(input)) > 512<<10 {
		return input, errors.New("项目配置格式或大小超出支持范围")
	}
	if input.Skills == nil {
		input.Skills = []protocol.ProjectSkill{}
	}
	if input.Scripts == nil {
		input.Scripts = []protocol.ProjectScript{}
	}
	if input.Workflows == nil {
		input.Workflows = []protocol.ProjectWorkflow{}
	}
	if input.Memory != nil && len(input.Memory.Content) > 64<<10 {
		return input, errors.New("项目记忆正文不能超过 64 KiB")
	}
	skills, scripts, workflows := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, skill := range input.Skills {
		if err := metadata(skill.ID, skill.Name, skill.Description, skills); err != nil {
			return input, err
		}
		if strings.TrimSpace(skill.Instructions) == "" || len(skill.Instructions) > 64<<10 {
			return input, errors.New("Skill 需要有效的指令正文")
		}
	}
	for _, script := range input.Scripts {
		if err := metadata(script.ID, script.Name, script.Description, scripts); err != nil {
			return input, err
		}
		bound := false
		for _, directory := range directories {
			bound = bound || directory.DeviceID == script.DeviceID && directory.ProjectID == script.ProjectID
		}
		if !bound || len(script.Command) == 0 || len(script.Command) > 256 || script.Command[0] == "" || script.TimeoutMS < 0 || script.TimeoutMS > 43200000 {
			return input, errors.New("脚本需要绑定本项目的执行目录并提供有效命令和超时")
		}
		for _, argument := range script.Command {
			if strings.ContainsRune(argument, 0) || len(argument) > 64<<10 {
				return input, errors.New("脚本参数含无效内容或超出大小限制")
			}
		}
		// A portable relative path is resolved using the selected node's syntax.
		if strings.ContainsAny(script.Cwd, "\\:\x00") || strings.HasPrefix(script.Cwd, "/") || path.Clean(script.Cwd) == ".." || strings.HasPrefix(path.Clean(script.Cwd), "../") {
			return input, errors.New("脚本工作目录需要使用项目内的相对路径和正斜线")
		}
	}
	for _, workflow := range input.Workflows {
		if err := metadata(workflow.ID, workflow.Name, workflow.Description, workflows); err != nil {
			return input, err
		}
		if len(workflow.Instructions) > 64<<10 || len(workflow.Steps) == 0 || len(workflow.Steps) > 32 {
			return input, errors.New("工作流需要包含 1 至 32 个脚本步骤")
		}
		for _, script := range workflow.Steps {
			if !scripts[script] {
				return input, errors.New("工作流引用了尚未定义的项目脚本")
			}
		}
	}
	return input, nil
}
