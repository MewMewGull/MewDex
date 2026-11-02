package runtimefiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeVersionsDoNotReplaceAProgramThatMayStillBeRunning(t *testing.T) {
	source := filepath.Join(t.TempDir(), "program")
	root := t.TempDir()
	if err := os.WriteFile(source, []byte("first build"), 0700); err != nil {
		t.Fatal(err)
	}
	first, err := Program(root, source)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Program(root, source)
	if err != nil || first != again {
		t.Fatal("unchanged runtime was staged under another identity")
	}
	if err = os.WriteFile(source, []byte("second build"), 0700); err != nil {
		t.Fatal(err)
	}
	second, err := Program(root, source)
	if err != nil || second == first {
		t.Fatal("updated runtime reused an active binary path")
	}
	before, _ := os.ReadFile(first)
	if string(before) != "first build" {
		t.Fatal("old service binary was replaced")
	}
}

func TestEngineRuntimePreservesItsCompanionsAndRejectsEscapingPaths(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	entry := filepath.Join(source, "bin", "codex")
	files := map[string][]byte{"bin/codex": []byte("fixture engine, never executed"), "codex-resources/helper": []byte("fixture companion")}
	manifest := Manifest{Format: 1, Version: "0.154.0", Target: "fixture"}
	for name, data := range files {
		path := filepath.Join(source, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, File{Path: name, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))})
	}
	writeManifest := func() {
		raw, _ := json.Marshal(manifest)
		if err := os.WriteFile(filepath.Join(source, "mewdex-engine.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest()
	staged, err := Engine(destination, entry, "0.154.0")
	if err != nil {
		t.Fatal(err)
	}
	companion := filepath.Join(filepath.Dir(filepath.Dir(staged)), "codex-resources", "helper")
	if data, err := os.ReadFile(companion); err != nil || string(data) != "fixture companion" {
		t.Fatal("native companion layout was lost")
	}
	if err = os.WriteFile(filepath.Join(source, "unlisted-private.txt"), []byte("must not be granted"), 0600); err != nil {
		t.Fatal(err)
	}
	resources, err := EngineResources(entry, "0.154.0")
	if err != nil || len(resources) != len(files) {
		t.Fatal("runtime resources differ from the verified manifest", err)
	}
	for _, resource := range resources {
		if filepath.Base(resource) == "unlisted-private.txt" {
			t.Fatal("an unlisted runtime file became readable")
		}
	}
	if err = os.WriteFile(filepath.Join(source, "codex-resources", "helper"), []byte("changed companion"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = EngineResources(entry, "0.154.0"); err == nil {
		t.Fatal("changed runtime companion was accepted for a read grant")
	}
	manifest.Files = append(manifest.Files, File{Path: "../outside", SHA256: "not-a-valid-digest"})
	writeManifest()
	if _, err = Engine(destination, entry, "0.154.0"); err == nil {
		t.Fatal("manifest could read outside its runtime directory")
	}
}
