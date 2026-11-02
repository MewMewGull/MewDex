// Package projectfiles reads current project files through directory handles.
// It cannot follow links or enter the executor's private paths.
package projectfiles

import (
	"context"
	"errors"
	"io"
	"mewdex/internal/protocol"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Root struct {
	path    string
	private []string
}

func New(path string, private []string) *Root { return &Root{path: path, private: private} }
func within(parent, path string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		parent, path = strings.ToLower(parent), strings.ToLower(path)
	}
	relative, err := filepath.Rel(parent, path)
	return err == nil && filepath.IsLocal(relative)
}
func (r *Root) allowed(relative string) bool {
	path := filepath.Join(r.path, relative)
	for _, private := range r.private {
		if within(private, path) {
			return false
		}
	}
	return true
}
func (r *Root) relative(path string) (string, error) {
	if path == "" {
		path = "."
	}
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(r.path, path)
		if err != nil {
			return "", errors.New("file is outside this project")
		}
	}
	path = filepath.Clean(filepath.FromSlash(path))
	if !filepath.IsLocal(path) || strings.ContainsRune(path, '\x00') || (runtime.GOOS == "windows" && strings.Contains(path, ":")) {
		return "", errors.New("choose a file inside this project")
	}
	if !r.allowed(path) {
		return "", errors.New("private device files cannot be read from the workbench")
	}
	return path, nil
}

// Each child is opened relative to its parent handle and compared with the
// observed identity, so a directory/link swap cannot redirect the read.
func (r *Root) directory(relative string) (*os.Root, error) {
	root, err := os.OpenRoot(r.path)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return root, nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		info, err := root.Lstat(part)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, errors.New("project directory is unavailable or is a link")
		}
		next, err := root.OpenRoot(part)
		root.Close()
		if err != nil {
			return nil, err
		}
		current, err := next.Stat(".")
		if err != nil || !os.SameFile(info, current) {
			next.Close()
			return nil, errors.New("project directory changed while opening")
		}
		root = next
	}
	return root, nil
}
func (r *Root) Open(path string) (*os.File, string, error) {
	relative, err := r.relative(path)
	if err != nil {
		return nil, "", err
	}
	root, err := r.directory(filepath.Dir(relative))
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	name := filepath.Base(relative)
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", errors.New("choose an existing regular project file; links are not exported")
	}
	file, err := openRead(root, name)
	if err != nil {
		return nil, "", err
	}
	current, err := file.Stat()
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		file.Close()
		return nil, "", errors.New("project file changed while opening")
	}
	return file, filepath.ToSlash(relative), nil
}
func (r *Root) List(ctx context.Context, path, after string) (protocol.FileListing, error) {
	page := protocol.FileListing{Entries: []protocol.ProjectFile{}}
	relative, err := r.relative(path)
	if err != nil {
		return page, err
	}
	root, err := r.directory(relative)
	if err != nil {
		return page, err
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return page, err
	}
	defer directory.Close()
	page.Path = filepath.ToSlash(relative)
	// Retain only the next 201 names, regardless of a directory's total size.
	names := []string{}
	for {
		if err = ctx.Err(); err != nil {
			return page, err
		}
		batch, readErr := directory.ReadDir(128)
		for _, entry := range batch {
			if entry.Name() > after && r.allowed(filepath.Join(relative, entry.Name())) && (entry.IsDir() || entry.Type().IsRegular()) && entry.Type()&os.ModeSymlink == 0 {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		if len(names) > 201 {
			names = names[:201]
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return page, readErr
		}
	}
	page.HasMore = len(names) > 200
	if page.HasMore {
		names = names[:200]
	}
	for _, name := range names {
		info, err := root.Lstat(name)
		if err != nil || !(info.IsDir() || info.Mode().IsRegular()) || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		page.Entries = append(page.Entries, protocol.ProjectFile{Name: name, Path: filepath.ToSlash(filepath.Join(relative, name)), Directory: info.IsDir(), Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339Nano)})
	}
	if len(names) > 0 {
		page.Next = names[len(names)-1]
	}
	return page, nil
}
