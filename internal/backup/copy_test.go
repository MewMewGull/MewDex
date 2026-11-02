package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStateCopyDoesNotReplaceExistingFilesOrIgnoreCancellation(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "state.db"), []byte("owned state"), 0600); err != nil {
		t.Fatal(err)
	}
	selections := []Selection{{Path: "state.db", Name: "data/state.db"}}
	files, err := CopyVerified(context.Background(), source, destination, selections, Options{})
	if err != nil || len(files) != 1 {
		t.Fatal(err)
	}
	target := filepath.Join(destination, "data", "state.db")
	if err = os.WriteFile(target, []byte("preserved host edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = CopyVerified(context.Background(), source, destination, selections, Options{}); !errors.Is(err, os.ErrExist) {
		t.Fatal("copy replaced an existing destination", err)
	}
	raw, _ := os.ReadFile(target)
	if string(raw) != "preserved host edit" {
		t.Fatal("copy lost existing data")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = CopyVerified(ctx, source, t.TempDir(), selections, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal("copy ignored cancelled context", err)
	}
}
