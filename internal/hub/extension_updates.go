package hub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

type extensionEdit struct {
	KeyPath       string `json:"keyPath"`
	Value         any    `json:"value"`
	MergeStrategy string `json:"mergeStrategy"`
}

func settingEdit(path string, value any) extensionEdit {
	return extensionEdit{KeyPath: path, Value: value, MergeStrategy: "replace"}
}

func writeExtensionConfig(ctx context.Context, runtime extensionRuntime, version string, edits []extensionEdit) (protocol.ExtensionMutation, error) {
	var result protocol.ExtensionMutation
	err := runtime.Call(ctx, "config/batchWrite", map[string]any{"edits": edits, "expectedVersion": version, "reloadUserConfig": true}, &result)
	return result, err
}

func updateExtension(ctx context.Context, runtime extensionRuntime, home, cwd string, request protocol.ExtensionRequest, attempt string) (protocol.ExtensionMutation, error) {
	result := protocol.ExtensionMutation{Status: "ok"}
	catalog, err := readExtensionCatalog(ctx, runtime, cwd, request.Section)
	if err != nil {
		return result, err
	}
	if request.ExpectedVersion == "" || request.ExpectedVersion != catalog.ConfigVersion {
		return result, errors.New("扩展配置已变化，请刷新后重试")
	}
	switch request.Action {
	case "skill.create":
		if request.Section != "skills" {
			break
		}
		for _, skill := range catalog.Skills {
			if skill.Name == request.Name {
				return result, errors.New("已存在同名 Skill，请使用其他编号")
			}
		}
		return result, createRuntimeSkill(home, request)
	case "skill.set":
		if request.Section != "skills" {
			break
		}
		for _, skill := range catalog.Skills {
			if skill.Path == request.Path {
				var state struct {
					Enabled bool `json:"effectiveEnabled"`
				}
				err = runtime.Call(ctx, "skills/config/write", map[string]any{"path": skill.Path, "enabled": request.Enabled}, &state)
				if err == nil && state.Enabled != request.Enabled {
					result.Status = "overridden"
				}
				return result, err
			}
		}
		return result, errors.New("Skill 目录已变化，请刷新后重试")
	case "feature.set":
		if request.Section != "hooks" && request.Section != "plugins" {
			break
		}
		return writeExtensionConfig(ctx, runtime, request.ExpectedVersion, []extensionEdit{settingEdit("features."+request.Section, request.Enabled)})
	case "memory.set":
		if request.Section != "memory" {
			break
		}
		return writeExtensionConfig(ctx, runtime, request.ExpectedVersion, []extensionEdit{
			settingEdit("features.memories", request.Enabled),
			settingEdit("memories.generate_memories", request.GenerateMemory),
			settingEdit("memories.use_memories", request.UseMemory),
		})
	case "hook.create":
		if request.Section != "hooks" {
			break
		}
		return createRuntimeHook(ctx, runtime, cwd, request)
	case "hook.set":
		if request.Section != "hooks" {
			break
		}
		for _, hook := range catalog.Hooks {
			if hook.Key != request.Key {
				continue
			}
			if hook.IsManaged || hook.CurrentHash != request.Hash {
				return result, errors.New("Hook 由管理员策略管理或内容已变化，请刷新后核对")
			}
			key := "hooks.state." + strconv.Quote(hook.Key)
			edits := []extensionEdit{settingEdit(key+".enabled", request.Enabled)}
			if request.Enabled {
				edits = append(edits, settingEdit(key+".trusted_hash", hook.CurrentHash))
			}
			return writeExtensionConfig(ctx, runtime, request.ExpectedVersion, edits)
		}
		return result, errors.New("Hook 已变化，请刷新后重试")
	case "marketplace.add":
		if request.Section != "plugins" {
			break
		}
		source := strings.TrimSpace(request.Source)
		if source == "" || !validExtensionText(source, 4096) || strings.ContainsAny(source, "\r\n") {
			return result, errors.New("请输入有效的插件来源")
		}
		if !filepath.IsAbs(source) {
			if err = config.ValidateURL(source, false); err != nil || !strings.HasPrefix(source, "https://") {
				return result, errors.New("插件来源需为 HTTPS 仓库地址或 Hub 上的绝对路径")
			}
		}
		err = runtime.Call(ctx, "marketplace/add", map[string]string{"source": source}, &struct{}{})
		return result, err
	case "plugin.install", "plugin.remove", "plugin.set":
		if request.Section != "plugins" {
			break
		}
		for _, plugin := range catalog.Plugins {
			if plugin.ID != request.PluginID {
				continue
			}
			switch request.Action {
			case "plugin.install":
				if plugin.Fingerprint != request.Hash || plugin.Source != request.Source || plugin.Marketplace != request.Marketplace || plugin.MarketplacePath != request.MarketplacePath || plugin.Name != request.Name || plugin.InstallPolicy == "NOT_AVAILABLE" || plugin.Availability != "" && plugin.Availability != "AVAILABLE" {
					return result, errors.New("插件来源或安装策略已变化，请刷新后核对")
				}
				params := map[string]any{"pluginName": plugin.Name, "installAttemptId": attempt}
				if plugin.MarketplacePath != "" {
					params["marketplacePath"] = plugin.MarketplacePath
				} else {
					params["remoteMarketplaceName"] = plugin.Marketplace
				}
				var installed struct {
					Apps []struct {
						Name string `json:"name"`
					} `json:"appsNeedingAuth"`
				}
				err = runtime.Call(ctx, "plugin/install", params, &installed)
				for _, app := range installed.Apps {
					result.AppsNeedingAuth = append(result.AppsNeedingAuth, app.Name)
				}
				return result, err
			case "plugin.remove":
				if plugin.InstallPolicy == "INSTALLED_BY_DEFAULT" {
					return result, errors.New("此插件由运行时策略提供")
				}
				err = runtime.Call(ctx, "plugin/uninstall", map[string]string{"pluginId": plugin.ID}, &struct{}{})
				return result, err
			case "plugin.set":
				if !plugin.Installed {
					return result, errors.New("请先安装此插件")
				}
				return writeExtensionConfig(ctx, runtime, request.ExpectedVersion, []extensionEdit{settingEdit("plugins."+strconv.Quote(plugin.ID)+".enabled", request.Enabled)})
			}
		}
		return result, errors.New("插件目录已变化，请刷新后重试")
	}
	return result, errors.New("扩展操作与分类不匹配")
}

var runtimeSkillName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func createRuntimeSkill(home string, request protocol.ExtensionRequest) error {
	if home == "" || !runtimeSkillName.MatchString(request.Name) || strings.TrimSpace(request.Description) == "" || !validExtensionText(request.Description, 4096) || strings.TrimSpace(request.Instructions) == "" || !validExtensionText(request.Instructions, 64<<10) {
		return errors.New("Skill 需要英文编号、说明和有效正文")
	}
	directory := filepath.Join(home, "skills", "mewdex-"+request.Name)
	if err := os.MkdirAll(filepath.Dir(directory), 0700); err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return errors.New("同名 Skill 目录已存在，请选择其他编号")
	}
	file, err := os.CreateTemp(directory, ".skill-write-")
	if err != nil {
		_ = os.Remove(directory)
		return err
	}
	defer os.Remove(file.Name())
	content := "---\nname: " + strconv.Quote(request.Name) + "\ndescription: " + strconv.Quote(request.Description) + "\n---\n\n" + request.Instructions + "\n"
	_, writeErr := file.WriteString(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		return errors.New("Skill 写入未完成，请刷新目录后重试")
	}
	if err := os.Rename(file.Name(), filepath.Join(directory, "SKILL.md")); err != nil {
		_ = os.Remove(file.Name())
		_ = os.Remove(directory)
		return err
	}
	return nil
}

func createRuntimeHook(ctx context.Context, runtime extensionRuntime, cwd string, request protocol.ExtensionRequest) (protocol.ExtensionMutation, error) {
	events := []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "PreCompact", "PostCompact", "SubagentStart", "SubagentStop", "Stop", "Interrupt"}
	if !slices.Contains(events, request.Event) || strings.TrimSpace(request.Command) == "" || !validExtensionText(request.Command, 16384) || !validExtensionText(request.Matcher, 4096) || request.TimeoutSec < 1 || request.TimeoutSec > 600 {
		return protocol.ExtensionMutation{}, errors.New("Hook 需要有效事件、命令和 1 至 600 秒超时")
	}
	configuration, version, err := readExtensionConfig(ctx, runtime, cwd)
	if err != nil {
		return protocol.ExtensionMutation{}, err
	}
	if version != request.ExpectedVersion {
		return protocol.ExtensionMutation{}, errors.New("扩展配置已变化，请刷新后重试")
	}
	groups := []json.RawMessage{}
	for _, layer := range configuration.Layers {
		if layer.Name.Type != "user" {
			continue
		}
		var hooks map[string]json.RawMessage
		if raw := layer.Config["hooks"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &hooks); err != nil {
				return protocol.ExtensionMutation{}, err
			}
		}
		if raw := hooks[request.Event]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return protocol.ExtensionMutation{}, err
			}
		}
	}
	if len(groups) >= 64 {
		return protocol.ExtensionMutation{}, errors.New("此事件最多配置 64 组 Hook")
	}
	group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": request.Command, "timeout": request.TimeoutSec}}}
	if request.Matcher != "" {
		group["matcher"] = request.Matcher
	}
	groups = append(groups, protocol.JSON(group))
	return writeExtensionConfig(ctx, runtime, request.ExpectedVersion, []extensionEdit{settingEdit("hooks."+request.Event, groups)})
}
