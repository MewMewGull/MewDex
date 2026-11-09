package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type extensionRuntime interface {
	Call(context.Context, string, any, any) error
}

type extensionConfig struct {
	Config struct {
		Features map[string]bool `json:"features"`
		Memories struct {
			Generate *bool `json:"generate_memories"`
			Use      *bool `json:"use_memories"`
		} `json:"memories"`
	} `json:"config"`
	Layers []struct {
		Name struct {
			Type string `json:"type"`
		} `json:"name"`
		Version string                     `json:"version"`
		Config  map[string]json.RawMessage `json:"config"`
	} `json:"layers"`
}

func readExtensionConfig(ctx context.Context, runtime extensionRuntime, cwd string) (extensionConfig, string, error) {
	var config extensionConfig
	err := runtime.Call(ctx, "config/read", map[string]any{"cwd": cwd, "includeLayers": true}, &config)
	for _, layer := range config.Layers {
		if layer.Name.Type == "user" {
			return config, layer.Version, err
		}
	}
	if err == nil {
		err = errors.New("Hub 运行时未提供可编辑的用户配置")
	}
	return config, "", err
}

func extensionCommand(ctx context.Context, runtime extensionRuntime, home, cwd string, command protocol.Command) protocol.CommandResult {
	request := command.Extension
	if request == nil || len(protocol.JSON(request)) > 128<<10 {
		return protocol.CommandResult{CommandID: command.ID, State: "failed", Error: "无效的扩展管理请求"}
	}
	var value any
	var err error
	if command.Kind == "extension.read" {
		value, err = readExtensionCatalog(ctx, runtime, cwd, request.Section)
	} else if command.Kind == "extension.update" {
		value, err = updateExtension(ctx, runtime, home, cwd, *request, command.ID)
	} else {
		err = errors.New("未知扩展操作")
	}
	if err != nil {
		if command.Kind == "extension.update" && engine.IsUnknownOutcome(err) {
			return protocol.CommandResult{CommandID: command.ID, State: "unknown", Error: err.Error()}
		}
		return protocol.CommandResult{CommandID: command.ID, State: "failed", Error: err.Error()}
	}
	return protocol.CommandResult{CommandID: command.ID, State: "completed", Data: protocol.JSON(value)}
}

func readExtensionCatalog(ctx context.Context, runtime extensionRuntime, cwd, section string) (protocol.ExtensionCatalog, error) {
	view := protocol.ExtensionCatalog{Section: section, Skills: []protocol.ManagedSkill{}, Hooks: []protocol.RuntimeHook{}, Plugins: []protocol.RuntimePlugin{}, Warnings: []string{}}
	config, version, err := readExtensionConfig(ctx, runtime, cwd)
	if err != nil {
		return view, err
	}
	view.ConfigVersion = version
	view.HooksEnabled, view.PluginsEnabled, view.MemoryEnabled = config.Config.Features["hooks"], config.Config.Features["plugins"], config.Config.Features["memories"]
	// Hooks and plugins default on in the pinned 0.162.1 runtime. Config/read
	// contains explicit feature values, so an absent key retains that default.
	if _, configured := config.Config.Features["hooks"]; !configured {
		view.HooksEnabled = true
		if legacy, present := config.Config.Features["codex_hooks"]; present {
			view.HooksEnabled = legacy
		}
	}
	if _, configured := config.Config.Features["plugins"]; !configured {
		view.PluginsEnabled = true
	}
	if _, configured := config.Config.Features["memories"]; !configured {
		view.MemoryEnabled = config.Config.Features["memory_tool"]
	}
	view.GenerateMemory, view.UseMemory = config.Config.Memories.Generate == nil || *config.Config.Memories.Generate, config.Config.Memories.Use == nil || *config.Config.Memories.Use
	switch section {
	case "skills":
		var result struct {
			Data []struct {
				Cwd    string                  `json:"cwd"`
				Skills []protocol.ManagedSkill `json:"skills"`
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			} `json:"data"`
		}
		err = runtime.Call(ctx, "skills/list", map[string]any{"cwds": []string{cwd}, "forceReload": true}, &result)
		for _, entry := range result.Data {
			if filepath.Clean(entry.Cwd) != filepath.Clean(cwd) {
				continue
			}
			view.Skills = append(view.Skills, entry.Skills...)
			for _, issue := range entry.Errors {
				view.Warnings = append(view.Warnings, issue.Message)
			}
		}
	case "hooks":
		var result struct {
			Data []struct {
				Cwd      string                 `json:"cwd"`
				Hooks    []protocol.RuntimeHook `json:"hooks"`
				Warnings []string               `json:"warnings"`
				Errors   []struct {
					Message string `json:"message"`
				} `json:"errors"`
			} `json:"data"`
		}
		err = runtime.Call(ctx, "hooks/list", map[string]any{"cwds": []string{cwd}}, &result)
		for _, entry := range result.Data {
			if filepath.Clean(entry.Cwd) != filepath.Clean(cwd) {
				continue
			}
			view.Hooks = append(view.Hooks, entry.Hooks...)
			view.Warnings = append(view.Warnings, entry.Warnings...)
			for _, issue := range entry.Errors {
				view.Warnings = append(view.Warnings, issue.Message)
			}
		}
	case "plugins":
		var result struct {
			Marketplaces []struct {
				Name    string `json:"name"`
				Path    string `json:"path"`
				Plugins []struct {
					ID            string          `json:"id"`
					Name          string          `json:"name"`
					Version       string          `json:"version"`
					LocalVersion  string          `json:"localVersion"`
					Source        json.RawMessage `json:"source"`
					Installed     bool            `json:"installed"`
					Enabled       bool            `json:"enabled"`
					InstallPolicy string          `json:"installPolicy"`
					Availability  string          `json:"availability"`
					Interface     *struct {
						ShortDescription string `json:"shortDescription"`
					} `json:"interface"`
				} `json:"plugins"`
			} `json:"marketplaces"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"marketplaceLoadErrors"`
		}
		err = runtime.Call(ctx, "plugin/list", map[string]any{"cwds": []string{cwd}, "forceRefetch": false}, &result)
		for _, market := range result.Marketplaces {
			for _, plugin := range market.Plugins {
				item := protocol.RuntimePlugin{ID: plugin.ID, Name: plugin.Name, Version: plugin.Version, Marketplace: market.Name, MarketplacePath: market.Path, Source: string(plugin.Source), Installed: plugin.Installed, Enabled: plugin.Enabled, InstallPolicy: plugin.InstallPolicy, Availability: plugin.Availability}
				if plugin.Interface != nil {
					item.Description = plugin.Interface.ShortDescription
				}
				if item.Version == "" {
					item.Version = plugin.LocalVersion
				}
				item.Fingerprint = store.Hash(string(protocol.JSON(item)))
				view.Plugins = append(view.Plugins, item)
			}
		}
		for _, issue := range result.Errors {
			view.Warnings = append(view.Warnings, issue.Message)
		}
	case "memory":
	default:
		err = fmt.Errorf("不支持的扩展分类 %q", section)
	}
	return view, err
}

func validExtensionText(value string, limit int) bool {
	return len(value) <= limit && !strings.ContainsRune(value, 0)
}
