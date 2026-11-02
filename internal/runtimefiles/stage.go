// Package runtimefiles stages immutable, integrity-checked local executable sets.
package runtimefiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type Manifest struct {
	Format  int    `json:"format"`
	Version string `json:"version"`
	Target  string `json:"target"`
	Files   []File `json:"files"`
}

func digest(reader io.Reader) (string, error) {
	value := sha256.New()
	if _, err := io.Copy(value, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(value.Sum(nil)), nil
}
func fileHash(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("runtime input must be a regular file")
	}
	return digest(file)
}

func copyOnce(root *os.Root, source *os.File, target, expected string) error {
	if existing, err := root.Open(target); err == nil {
		actual, hashErr := fileHash(existing)
		existing.Close()
		if hashErr != nil {
			return hashErr
		}
		if actual != expected {
			return errors.New("cached runtime failed its integrity check; its files were preserved")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	output, err := root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	defer output.Close()
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(output, hash), source); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("runtime changed while being staged")
	}
	return output.Sync()
}

func Program(directory, source string) (string, error) {
	file, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash, err := fileHash(file)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	target := filepath.Join("runtimes", hash, filepath.Base(source))
	if err = copyOnce(root, file, target, hash); err != nil {
		return "", err
	}
	return filepath.Join(directory, target), nil
}

type engineSource struct {
	directory    string
	input        *os.Root
	manifestFile *os.File
	raw          []byte
	manifest     Manifest
	entry        string
}

func (s *engineSource) close() {
	if s.manifestFile != nil {
		s.manifestFile.Close()
	}
	s.input.Close()
}

func openEngine(source, version string) (_ *engineSource, resultErr error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	sourceDirectory := filepath.Dir(filepath.Dir(source))
	input, err := os.OpenRoot(sourceDirectory)
	if err != nil {
		return nil, err
	}
	set := &engineSource{directory: sourceDirectory, input: input}
	defer func() {
		if resultErr != nil {
			set.close()
		}
	}()
	manifestFile, err := input.Open("mewdex-engine.json")
	if err != nil {
		return nil, fmt.Errorf("bundled engine manifest is missing: %w", err)
	}
	set.manifestFile = manifestFile
	raw, err := io.ReadAll(io.LimitReader(manifestFile, 256<<10+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 256<<10 {
		return nil, errors.New("bundled engine manifest is too large")
	}
	var manifest Manifest
	if json.Unmarshal(raw, &manifest) != nil || manifest.Format != 1 || manifest.Version == "" || (version != "" && manifest.Version != version) || len(manifest.Files) == 0 {
		return nil, errors.New("bundled engine manifest does not match the pinned version")
	}
	relative, err := filepath.Rel(sourceDirectory, source)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	found := false
	for _, spec := range manifest.Files {
		path := filepath.FromSlash(spec.Path)
		if !filepath.IsLocal(path) || path == "." || strings.Contains(spec.Path, "\\") || len(spec.SHA256) != 64 || seen[path] {
			return nil, errors.New("invalid engine runtime manifest path or digest")
		}
		seen[path] = true
		file, err := input.Open(path)
		if err != nil {
			return nil, err
		}
		actual, hashErr := fileHash(file)
		info, statErr := file.Stat()
		file.Close()
		if hashErr != nil || statErr != nil || actual != spec.SHA256 || info.Size() != spec.Bytes {
			return nil, errors.New("bundled engine runtime failed its integrity check")
		}
		found = found || path == relative
	}
	if !found {
		return nil, errors.New("engine entry point is not listed in its manifest")
	}
	set.raw, set.manifest, set.entry = raw, manifest, relative
	return set, nil
}

// EngineResources returns only the verified, listed regular files. Callers can
// grant runtime reads without exposing the entire installation or account home.
func EngineResources(source, version string) ([]string, error) {
	if version == "" {
		return nil, errors.New("a pinned engine version is required")
	}
	set, err := openEngine(source, version)
	if err != nil {
		return nil, err
	}
	defer set.close()
	paths := make([]string, 0, len(set.manifest.Files))
	for _, spec := range set.manifest.Files {
		paths = append(paths, filepath.Join(set.directory, filepath.FromSlash(spec.Path)))
	}
	return paths, nil
}

func Engine(directory, source, version string) (string, error) {
	if version == "" {
		return "", errors.New("a pinned engine version is required")
	}
	set, err := openEngine(source, version)
	if err != nil {
		return "", err
	}
	defer set.close()
	if err = os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	output, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer output.Close()
	key := sha256.Sum256(set.raw)
	targetRoot := filepath.Join("engines", version+"-"+hex.EncodeToString(key[:16]))
	for _, spec := range set.manifest.Files {
		file, err := set.input.Open(filepath.FromSlash(spec.Path))
		if err != nil {
			return "", err
		}
		err = copyOnce(output, file, filepath.Join(targetRoot, filepath.FromSlash(spec.Path)), spec.SHA256)
		file.Close()
		if err != nil {
			return "", err
		}
	}
	if err = copyOnce(output, set.manifestFile, filepath.Join(targetRoot, "mewdex-engine.json"), hex.EncodeToString(key[:])); err != nil {
		return "", err
	}
	return filepath.Join(directory, targetRoot, set.entry), nil
}
