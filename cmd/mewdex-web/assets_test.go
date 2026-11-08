package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStartupStagesUIByContentAcrossUIOnlyUpgrades(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	index := filepath.Join(source, "index.html")
	if err := os.WriteFile(index, []byte("first UI"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := stageUI(root, source)
	if err != nil {
		t.Fatal(err)
	}
	again, err := stageUI(root, source)
	if err != nil || again != first {
		t.Fatal("identical assets were not reused", err)
	}
	if err = os.WriteFile(index, []byte("second UI"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := stageUI(root, source)
	if err != nil || second == first {
		t.Fatal("a UI-only upgrade reused its predecessor", err)
	}
	raw, err := os.ReadFile(filepath.Join(first, "index.html"))
	if err != nil || string(raw) != "first UI" {
		t.Fatal("upgrading changed previous assets", err)
	}
	if err = os.WriteFile(filepath.Join(second, "index.html"), []byte("modified staged assets"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = stageUI(root, source); err == nil {
		t.Fatal("modified startup assets were accepted")
	}
}
