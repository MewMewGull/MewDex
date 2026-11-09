package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/projectharness"
	"mewdex/internal/protocol"
)

type extensionRuntimeFunc func(context.Context, string, any, any) error

func (call extensionRuntimeFunc) Call(ctx context.Context, method string, params, result any) error {
	return call(ctx, method, params, result)
}

func TestExtensionMutationKeepsUnknownOutcomeAndRejectsStaleVersion(t *testing.T) {
	writes := 0
	runtime := extensionRuntimeFunc(func(_ context.Context, method string, _ any, result any) error {
		if method == "config/read" {
			return json.Unmarshal([]byte(`{"config":{"features":{}},"layers":[{"name":{"type":"user"},"version":"current","config":{}}]}`), result)
		}
		writes++
		return &engine.UnknownOutcome{Cause: context.DeadlineExceeded}
	})
	command := protocol.Command{ID: "same-write", Kind: "extension.update", Extension: &protocol.ExtensionRequest{Section: "memory", Action: "memory.set", ExpectedVersion: "current", Enabled: true}}
	result := extensionCommand(context.Background(), runtime, "", "", command)
	if result.CommandID != command.ID || result.State != "unknown" || writes != 1 {
		t.Fatal("uncertain native mutation was treated as a definite failure", result, writes)
	}
	command.Extension.ExpectedVersion = "stale"
	result = extensionCommand(context.Background(), runtime, "", "", command)
	if result.State != "failed" || writes != 1 {
		t.Fatal("stale extension settings were written", result, writes)
	}
}

func TestExtensionCatalogHonorsLegacyRuntimeFeatureNames(t *testing.T) {
	runtime := extensionRuntimeFunc(func(_ context.Context, _ string, _ any, result any) error {
		return json.Unmarshal([]byte(`{"config":{"features":{"codex_hooks":false,"memory_tool":true}},"layers":[{"name":{"type":"user"},"version":"current","config":{}}]}`), result)
	})
	view, err := readExtensionCatalog(context.Background(), runtime, "", "memory")
	if err != nil || view.HooksEnabled || !view.MemoryEnabled || !view.PluginsEnabled {
		t.Fatal("legacy runtime settings changed when viewed in MewDex", view, err)
	}
}

func TestExtensionFailureFinishesOriginalHubReceipt(t *testing.T) {
	h, _, server, admin := testHub(t)
	h.coordinator = &Coordinator{hub: h, root: t.TempDir()}
	command := protocol.Command{ID: "extension-invalid-request", DeviceID: h.coordinatorID, Kind: "extension.read"}
	response := request(t, server.URL, admin, "POST", "/v1/commands", command)
	response.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response = request(t, server.URL, admin, "GET", "/v1/commands/"+command.ID, nil)
		var result protocol.CommandResult
		err := json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if result.State == "failed" {
			if result.CommandID != command.ID {
				t.Fatal("extension result was stored under another command identity")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("extension operation left its original receipt accepted")
}

func TestExtensionManagementRequiresAdministratorAndHubTarget(t *testing.T) {
	h, _, _, _ := testHub(t)
	admin := protocol.Identity{Role: "admin", Access: protocol.Access{AllDevices: true}}
	client := protocol.Identity{Role: "client", Access: protocol.Access{AllDevices: true}}
	command := protocol.Command{DeviceID: h.coordinatorID, Kind: "extension.read", Extension: &protocol.ExtensionRequest{Section: "skills"}}
	if !h.allowsCommand(admin, command) || h.allowsCommand(client, command) {
		t.Fatal("extension catalog permissions were not restricted to Hub administration")
	}
	command.Kind = "extension.update"
	if !h.allowsCommand(admin, command) || h.allowsCommand(client, command) {
		t.Fatal("ordinary input could modify Hub extensions")
	}
	command.DeviceID = "remote-executor"
	if h.allowsCommand(admin, command) {
		t.Fatal("extension changes could target an execution node")
	}
	command.DeviceID, command.Kind = h.coordinatorID, "start"
	if h.allowsCommand(admin, command) {
		t.Fatal("extension payload was accepted as an unrelated command")
	}
}

func TestProjectMemoryIsOptionalVersionedAndExcludedWhenDisabled(t *testing.T) {
	configuration := projectharness.Empty()
	if strings.Contains(string(protocol.JSON(configuration)), "memory") {
		t.Fatal("old project configurations changed their content hash")
	}
	configuration.Memory = &protocol.ProjectMemory{Enabled: true, Content: "PROJECT_MEMORY_MARKER"}
	if _, err := projectharness.Normalize(configuration, nil); err != nil {
		t.Fatal(err)
	}
	state := protocol.ProjectHarnessState{Revision: 1, Configuration: configuration}
	if !strings.Contains(appendProjectConfiguration("original", state), "PROJECT_MEMORY_MARKER") {
		t.Fatal("enabled project memory did not reach the model context")
	}
	configuration.Memory.Enabled = false
	if strings.Contains(appendProjectConfiguration("original", state), "PROJECT_MEMORY_MARKER") {
		t.Fatal("disabled project memory leaked into the model context")
	}
	if state.Configuration.Memory.Content != "PROJECT_MEMORY_MARKER" {
		t.Fatal("rendering removed stored project memory")
	}
	configuration.Memory.Content = strings.Repeat("x", 64<<10+1)
	if _, err := projectharness.Normalize(configuration, nil); err == nil {
		t.Fatal("oversized project memory was accepted")
	}
}

func TestNativeExtensionManagement(t *testing.T) {
	program := os.Getenv("MEWDEX_NATIVE_EXTENSION_ENGINE")
	if program == "" {
		t.Skip("set MEWDEX_NATIVE_EXTENSION_ENGINE for the pinned, isolated runtime integration test")
	}
	home, cwd := t.TempDir(), t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runtime, err := engine.Start(ctx, config.Engine{Executable: program, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	h, _, server, admin := testHub(t)
	h.coordinator = &Coordinator{hub: h, root: cwd, engine: runtime, config: config.Config{Engine: config.Engine{Home: home}}}
	command := protocol.Command{ID: "native-extension-read", DeviceID: h.coordinatorID, Kind: "extension.read", Extension: &protocol.ExtensionRequest{Section: "skills"}}
	response := request(t, server.URL, admin, "POST", "/v1/commands", command)
	response.Body.Close()
	for deadline := time.Now().Add(3 * time.Second); ; {
		response = request(t, server.URL, admin, "GET", "/v1/commands/"+command.ID, nil)
		var result protocol.CommandResult
		err = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if result.State == "completed" {
			break
		}
		if result.State == "failed" || time.Now().After(deadline) {
			t.Fatal("native extension read did not complete through the normal Hub command route", result)
		}
		time.Sleep(10 * time.Millisecond)
	}
	read := func(section string) protocol.ExtensionCatalog {
		t.Helper()
		view, err := readExtensionCatalog(ctx, runtime, cwd, section)
		if err != nil {
			t.Fatal(section, err)
		}
		return view
	}
	update := func(view protocol.ExtensionCatalog, request protocol.ExtensionRequest) {
		t.Helper()
		request.Section, request.ExpectedVersion = view.Section, view.ConfigVersion
		if _, err := updateExtension(ctx, runtime, home, cwd, request, protocol.ID()); err != nil {
			t.Fatal(request.Action, err)
		}
	}
	view := read("skills")
	update(view, protocol.ExtensionRequest{Action: "skill.create", Name: "extension-fixture", Description: "Isolated skill test", Instructions: "Use this fixture guidance."})
	view = read("skills")
	var skill protocol.ManagedSkill
	for _, entry := range view.Skills {
		if entry.Name == "extension-fixture" {
			skill = entry
		}
	}
	if skill.Path == "" || !skill.Enabled {
		t.Fatal("created skill did not enter the native catalog", view.Warnings)
	}
	update(view, protocol.ExtensionRequest{Action: "skill.set", Path: skill.Path, Enabled: false})
	view = read("skills")
	for _, entry := range view.Skills {
		if entry.Name == skill.Name && entry.Enabled {
			t.Fatal("skill disable did not persist")
		}
	}
	view = read("memory")
	update(view, protocol.ExtensionRequest{Action: "memory.set", Enabled: true, GenerateMemory: false, UseMemory: true})
	view = read("memory")
	if !view.MemoryEnabled || view.GenerateMemory || !view.UseMemory {
		t.Fatal("memory configuration did not round-trip", view)
	}
	view = read("hooks")
	update(view, protocol.ExtensionRequest{Action: "hook.create", Event: "UserPromptSubmit", Command: "echo mewdex-extension-fixture", TimeoutSec: 5})
	view = read("hooks")
	if len(view.Hooks) != 1 || view.Hooks[0].CurrentHash == "" || view.Hooks[0].TrustStatus != "untrusted" {
		t.Fatal("new hook was not discoverable and untrusted", view)
	}
	hook := view.Hooks[0]
	update(view, protocol.ExtensionRequest{Action: "hook.set", Key: hook.Key, Hash: hook.CurrentHash, Enabled: true})
	view = read("hooks")
	if !view.Hooks[0].Enabled || view.Hooks[0].TrustStatus != "trusted" {
		t.Fatal("hook trust did not bind to its actual content", view)
	}
	stale := protocol.ExtensionRequest{Section: "hooks", Action: "hook.set", ExpectedVersion: view.ConfigVersion, Key: hook.Key, Hash: "old-content", Enabled: true}
	if _, err := updateExtension(ctx, runtime, home, cwd, stale, protocol.ID()); err == nil {
		t.Fatal("changed hook content was trusted without review")
	}
	update(view, protocol.ExtensionRequest{Action: "hook.set", Key: hook.Key, Hash: hook.CurrentHash, Enabled: false})
	if read("hooks").Hooks[0].Enabled {
		t.Fatal("hook disable did not persist")
	}
	market := t.TempDir()
	for _, directory := range []string{".agents/plugins", "demo/.codex-plugin", "demo/skills/plugin-fixture"} {
		if err := os.MkdirAll(filepath.Join(market, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		".agents/plugins/marketplace.json":    `{"name":"mewdex-fixture","plugins":[{"name":"demo","source":{"source":"local","path":"./demo"}}]}`,
		"demo/.codex-plugin/plugin.json":      `{"name":"demo","version":"1.0.0","description":"Isolated plugin test","skills":"./skills"}`,
		"demo/skills/plugin-fixture/SKILL.md": "---\nname: plugin-fixture\ndescription: Isolated plugin skill\n---\nUse this plugin fixture.\n",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(market, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	view = read("plugins")
	update(view, protocol.ExtensionRequest{Action: "marketplace.add", Source: market})
	view = read("plugins")
	if len(view.Plugins) != 1 {
		t.Fatal("added marketplace did not expose its plugin", view)
	}
	plugin := view.Plugins[0]
	update(view, protocol.ExtensionRequest{Action: "plugin.install", PluginID: plugin.ID, Name: plugin.Name, Hash: plugin.Fingerprint, Source: plugin.Source, Marketplace: plugin.Marketplace, MarketplacePath: plugin.MarketplacePath})
	view = read("plugins")
	if !view.Plugins[0].Installed {
		t.Fatal("plugin install did not persist")
	}
	found := false
	installedSkills := read("skills")
	for _, entry := range installedSkills.Skills {
		found = found || entry.Name == "demo:plugin-fixture" && entry.Enabled
	}
	if !found {
		t.Fatal("installed plugin did not provide its namespaced native Skill", installedSkills.Warnings)
	}
	update(view, protocol.ExtensionRequest{Action: "plugin.set", PluginID: plugin.ID, Enabled: false})
	view = read("plugins")
	if view.Plugins[0].Enabled {
		t.Fatal("plugin disable did not persist")
	}
	update(view, protocol.ExtensionRequest{Action: "plugin.remove", PluginID: plugin.ID})
	if read("plugins").Plugins[0].Installed {
		t.Fatal("plugin uninstall did not persist")
	}
	t.Log("Native Skills, Memory, Hooks and Plugins round-tripped. Model turns 0.")
}
