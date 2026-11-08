//go:build linux

package linuxuserns

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRequiresExplicitReviewedDigestBeforeAnyHostMutation(t *testing.T) {
	for _, input := range []struct {
		hash      string
		confirmed bool
	}{{"", false}, {BubblewrapSHA256, false}, {"wrong", true}} {
		if _, err := Apply(context.Background(), "/nonexistent", input.hash, input.confirmed); err == nil {
			t.Fatal("unreviewed preparation was accepted")
		}
	}
}

func TestPreparationRejectsModifiedHelperAndUnsafeFiles(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "bwrap")
	if err := os.WriteFile(file, []byte("untrusted helper"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := pinnedBytes(file); err == nil {
		t.Fatal("helper pin mismatch was accepted")
	}
	if err := os.Chmod(file, 0666); err != nil {
		t.Fatal(err)
	}
	if err := trusted(file, false); err == nil {
		t.Fatal("writable preparation file was accepted")
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := trusted(link, false); err == nil {
		t.Fatal("preparation symlink was accepted")
	}
	if err := trusted(directory, false); err == nil {
		t.Fatal("directory was accepted as helper executable")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := trusted(directory, true); err == nil {
		t.Fatal("preparation directory cannot be traversed by executor accounts")
	}
}

func TestPreparationFileInstallRetainsReviewedPreexistingContent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("positive root-ownership fixture requires the Linux root test host")
	}
	directory := t.TempDir()
	file := filepath.Join(directory, "profile")
	created, err := installFile(file, []byte("reviewed"), 0644)
	if err != nil || !created {
		t.Fatal(created, err)
	}
	created, err = installFile(file, []byte("reviewed"), 0644)
	if err != nil || created {
		t.Fatal("idempotent preparation changed an existing file", created, err)
	}
	if _, err = installFile(file, []byte("different"), 0644); err == nil {
		t.Fatal("existing configuration was replaced")
	}
	raw, _ := os.ReadFile(file)
	if string(raw) != "reviewed" {
		t.Fatal("existing profile content changed")
	}
}
