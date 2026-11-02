package projectfiles

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectFileBoundariesAndPagination(t *testing.T) {
	project := t.TempDir()
	private := filepath.Join(project, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "account.json"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := range 205 {
		if err := os.WriteFile(filepath.Join(project, fmt.Sprintf("report-%03d.txt", i)), []byte("project output"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root := New(project, []string{private})
	first, err := root.List(context.Background(), ".", "")
	if err != nil || len(first.Entries) != 200 || !first.HasMore {
		t.Fatalf("first page: %+v %v", first, err)
	}
	second, err := root.List(context.Background(), ".", first.Next)
	if err != nil || len(second.Entries) != 5 || second.HasMore {
		t.Fatalf("second page: %+v %v", second, err)
	}
	if first.Entries[0].Name != "report-000.txt" || second.Entries[4].Name != "report-204.txt" {
		t.Fatal("directory cursor did not preserve order")
	}
	for _, path := range []string{"../outside.txt", filepath.Join(private, "account.json"), "private/account.json"} {
		if file, _, err := root.Open(path); err == nil {
			file.Close()
			t.Fatalf("opened excluded path %q", path)
		}
	}
	file, relative, err := root.Open(filepath.Join(project, "report-000.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "project output" || relative != "report-000.txt" {
		t.Fatal(relative, string(data), err)
	}
	if _, err := root.List(context.Background(), "private", ""); err == nil {
		t.Fatal("listed private directory")
	}
}

func TestProjectFileLinksAreNotFollowed(t *testing.T) {
	project, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(project, "alias")); err != nil {
		t.Skip("native symlinks unavailable:", err)
	}
	root := New(project, nil)
	if file, _, err := root.Open("alias/secret"); err == nil {
		file.Close()
		t.Fatal("followed a directory link")
	}
	if _, err := root.List(context.Background(), "alias", ""); err == nil {
		t.Fatal("listed a linked directory")
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(project, "file-link")); err != nil {
		t.Fatal(err)
	}
	if file, _, err := root.Open("file-link"); err == nil {
		file.Close()
		t.Fatal("followed a file link")
	}
}
