package attachments

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mewdex/internal/protocol"
)

func TestHistoryIndexRebuildsCurrentPathsAndPreservesChangedFiles(t *testing.T) {
	assets, view, meta, root := viewFixture(t)
	path, err := view.StageIndex(context.Background(), assets, []string{meta.ID, meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	var index struct {
		Target ViewTarget                                `json:"target"`
		Files  []struct{ ID, Name, Path, SHA256 string } `json:"files"`
	}
	if err != nil || json.Unmarshal(raw, &index) != nil || index.Target != view.target || len(index.Files) != 1 || index.Files[0].ID != meta.ID || index.Files[0].Name != meta.Name || index.Files[0].SHA256 != meta.SHA256 || index.Files[0].Path != filepath.Join(view.Directory(), meta.ID) {
		t.Fatal("index lost its binding or current attachment path", err)
	}
	newParent := filepath.Join(root, "restored-view-parent")
	if err = os.Mkdir(newParent, 0700); err != nil {
		t.Fatal(err)
	}
	restored, err := NewView(filepath.Join(newParent, view.target.Key()), view.target)
	if err != nil {
		t.Fatal(err)
	}
	restoredIndex, err := restored.StageIndex(context.Background(), assets, []string{meta.ID})
	if err != nil || restoredIndex == path {
		t.Fatal("reused the old local index", err)
	}
	newRaw, err := os.ReadFile(restoredIndex)
	if err != nil || json.Unmarshal(newRaw, &index) != nil || len(index.Files) != 1 || index.Files[0].Path != filepath.Join(restored.Directory(), meta.ID) {
		t.Fatal("restored index is invalid", err)
	}
	if err = os.WriteFile(restoredIndex, []byte("changed index"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.StageIndex(context.Background(), assets, []string{meta.ID}); err == nil {
		t.Fatal("overwrote a changed history index")
	}
	preserved, _ := os.ReadFile(restoredIndex)
	if string(preserved) != "changed index" {
		t.Fatal("lost changed index evidence")
	}
}

func viewFixture(t *testing.T) (*Store, *View, protocol.Attachment, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	assets, err := New(filepath.Join(root, "private-cache"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := assets.SaveTarget("用户附件.txt", strings.NewReader("original user input"), "phone", "device", "project")
	if err != nil {
		t.Fatal(err)
	}
	target := ViewTarget{DeviceID: "device", ProjectID: "project", SessionID: "session"}
	view, err := NewView(filepath.Join(root, target.Key()), target)
	if err != nil {
		t.Fatal(err)
	}
	return assets, view, meta, root
}

func TestSessionViewStagesAnImmutableVerifiedCopy(t *testing.T) {
	assets, view, meta, _ := viewFixture(t)
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			path, err := view.Stage(assets, meta.ID)
			if err != nil {
				t.Error(err)
				return
			}
			raw, err := os.ReadFile(path)
			if err != nil || string(raw) != "original user input" {
				t.Error("staged input mismatch", err)
			}
		})
	}
	group.Wait()
	resumed, err := NewView(view.Directory(), view.target)
	if err != nil {
		t.Fatal(err)
	}
	path, err := resumed.Stage(assets, meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("unexpected host edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = resumed.Stage(assets, meta.ID); err == nil {
		t.Fatal("silently replaced an altered cached input")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "unexpected host edit" {
		t.Fatal("changed evidence was lost")
	}
	original, _ := assets.Path(meta.ID)
	raw, _ = os.ReadFile(original)
	if string(raw) != "original user input" {
		t.Fatal("original attachment changed")
	}
}

func TestSessionViewDoesNotAdoptAnotherTargetOrDirectory(t *testing.T) {
	assets, view, meta, root := viewFixture(t)
	other := view.target
	other.ProjectID = "another-project"
	if _, err := NewView(view.Directory(), other); err == nil {
		t.Fatal("adopted another session's view")
	}
	foreign, err := NewView(filepath.Join(root, other.Key()), other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = foreign.Stage(assets, meta.ID); err == nil {
		t.Fatal("staged another project's cached input")
	}
	markerless := filepath.Join(root, "existing-directory")
	if err = os.Mkdir(markerless, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = NewView(markerless, other); err == nil {
		t.Fatal("adopted an unowned directory")
	}
	if err = os.Rename(view.Directory(), filepath.Join(root, "retained-original-view")); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(view.Directory(), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = view.Stage(assets, meta.ID); err == nil {
		t.Fatal("wrote into a substituted directory")
	}
}

func TestInputViewRejectsCorruptedOriginalAndEscapingLinks(t *testing.T) {
	t.Run("corrupted-original", func(t *testing.T) {
		assets, view, meta, _ := viewFixture(t)
		original, _ := assets.Path(meta.ID)
		if err := os.WriteFile(original, []byte("altered user input!"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := view.Stage(assets, meta.ID); err == nil {
			t.Fatal("accepted an altered original")
		}
		if _, err := os.Stat(filepath.Join(view.Directory(), meta.ID)); !os.IsNotExist(err) {
			t.Fatal("published a failed copy")
		}
	})
	t.Run("escaping-original-link", func(t *testing.T) {
		assets, view, meta, root := viewFixture(t)
		outside := filepath.Join(root, "outside-cache.txt")
		if err := os.WriteFile(outside, []byte("original user input"), 0600); err != nil {
			t.Fatal(err)
		}
		original, _ := assets.Path(meta.ID)
		if err := os.Remove(original); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, original); err != nil {
			t.Skip("symbolic links unavailable:", err)
		}
		if _, err := view.Stage(assets, meta.ID); err == nil {
			t.Fatal("read through an escaping cache link")
		}
	})
}
