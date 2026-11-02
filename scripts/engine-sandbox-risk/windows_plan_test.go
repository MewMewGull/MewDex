package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
)

func setupPlanFixture(t *testing.T) (string, string, windowsPlan) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mainText, companionText := "retained engine", "retained setup helper"
	manifest := runtimefiles.Manifest{Format: 1, Version: engine.CompatibleVersion, Target: "test", Files: []runtimefiles.File{}}
	for _, item := range []struct{ path, text string }{{"bin/codex.exe", mainText}, {"bin/codex-windows-sandbox.exe", companionText}} {
		digest, _ := hashReader(strings.NewReader(item.text))
		manifest.Files = append(manifest.Files, runtimefiles.File{Path: item.path, SHA256: digest, Bytes: int64(len(item.text))})
	}
	raw := protocol.JSON(manifest)
	manifestHash, _ := hashReader(strings.NewReader(string(raw)))
	runtimeRoot := filepath.Join(root, "engines", engine.CompatibleVersion+"-"+manifestHash[:32])
	program := filepath.Join(runtimeRoot, "bin", "codex.exe")
	write(program, mainText)
	write(filepath.Join(runtimeRoot, "bin", "codex-windows-sandbox.exe"), companionText)
	write(filepath.Join(runtimeRoot, "mewdex-engine.json"), string(raw))
	home, workspace := filepath.Join(root, "engine-home"), filepath.Join(root, "workspace")
	write(filepath.Join(home, "config.toml"), "fixture configuration")
	if err = os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "preparation-helper.exe")
	write(helper, "retained preparation tool")
	plan := windowsPlan{Format: 1, Root: root, Home: home, Workspace: workspace, Program: program, EngineVersion: engine.CompatibleVersion, EngineManifestSHA: manifestHash}
	plan.ProgramSHA256, _ = hashFile(program)
	plan.ConfigSHA256, _ = hashFile(filepath.Join(home, "config.toml"))
	plan.HelperSHA256, _ = hashFile(helper)
	path := filepath.Join(root, "windows-setup-plan.json")
	write(path, string(protocol.JSON(plan)))
	return path, helper, plan
}

func TestRetainedSetupRejectsEscapesAndChangedCompanions(t *testing.T) {
	for _, mutation := range []string{"none", "outside-program", "companion", "config", "helper", "wrong-cwd"} {
		t.Run(mutation, func(t *testing.T) {
			path, helper, plan := setupPlanFixture(t)
			switch mutation {
			case "outside-program":
				plan.Program = helper
				plan.ProgramSHA256 = plan.HelperSHA256
				if err := os.WriteFile(path, protocol.JSON(plan), 0600); err != nil {
					t.Fatal(err)
				}
			case "companion":
				if err := os.WriteFile(filepath.Join(filepath.Dir(plan.Program), "codex-windows-sandbox.exe"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "config":
				if err := os.WriteFile(filepath.Join(plan.Home, "config.toml"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "helper":
				if err := os.WriteFile(helper, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := validateWindowsPlan(path, helper, mutation == "wrong-cwd")
			if (err == nil) != (mutation == "none") {
				t.Fatalf("mutation %s, validation error %v", mutation, err)
			}
			if _, err := os.Stat(filepath.Join(plan.Root, "setup-attempt.json")); !os.IsNotExist(err) {
				t.Fatal("validation must never request setup")
			}
		})
	}
}

func TestSetupNotificationRequiresExplicitSuccess(t *testing.T) {
	for _, raw := range []string{`{}`, `{"mode":"elevated"}`, `{"mode":"unelevated","success":true}`, `{"mode":"elevated","success":false,"error":"declined"}`, `not json`} {
		if err := checkSetupCompletion(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if err := checkSetupCompletion(json.RawMessage(`{"mode":"elevated","success":true}`)); err != nil {
		t.Fatal(err)
	}
}
