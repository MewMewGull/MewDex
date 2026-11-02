package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverlapSeparatesSiblingsFromAnAncestorWorkspace(t *testing.T) {
	root := t.TempDir()
	a, child, sibling := filepath.Join(root, "work"), filepath.Join(root, "work", "child"), filepath.Join(root, "worker")
	for _, dir := range []string{child, sibling} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if !Overlap(a, child) || !Overlap(child, a) || !Overlap(a, a) {
		t.Fatal("an overlapping workspace was treated as independent")
	}
	if Overlap(a, sibling) || Overlap(sibling, child) {
		t.Fatal("a filename prefix was treated as a parent directory")
	}
	if !Overlap("", a) {
		t.Fatal("unknown prior workspace was ignored")
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(a, alias); err != nil {
		t.Log("symlink coverage unavailable:", err)
		return
	}
	real, err := CanonicalDir(alias)
	if err != nil || !Overlap(real, a) {
		t.Fatalf("directory alias escaped scheduling: %s %v", real, err)
	}
}
