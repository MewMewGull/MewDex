// Package backup stores a bounded, authenticated archive independently of any
// service or GUI. Callers select the owned state to include and hold its locks.
package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"filippo.io/age"
)

const maxManifestBytes = 8 << 20
const passwordWorkFactor = 18

type File struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Format    int             `json:"format"`
	Kind      string          `json:"kind"`
	CreatedAt string          `json:"createdAt"`
	Metadata  json.RawMessage `json:"metadata"`
	Files     []File          `json:"files"`
}
type Selection struct{ Path, Name string }
type Limits struct {
	MaxFiles int
	MaxBytes int64
}
type Options struct {
	Limits Limits
	// Exclude sees archive-relative names. Excluded directories are not read.
	Exclude     func(string) bool
	IncludeFile func(string) bool
	Report      func(Progress)
}

// Progress describes the current phase, rather than a guessed overall percent.
// Totals are zero until a complete manifest is available. No file names or
// credentials are included. Callbacks run synchronously on the caller's worker.
type Progress struct {
	Phase      string
	Files      int
	TotalFiles int
	Bytes      int64
	TotalBytes int64
}

func report(callback func(Progress), value Progress) {
	if callback != nil {
		callback(value)
	}
}

type progressWriter struct{ add func(int) }

func (w progressWriter) Write(value []byte) (int, error) {
	if w.add != nil {
		w.add(len(value))
	}
	return len(value), nil
}

func (l Limits) normalized() (Limits, error) {
	if l.MaxFiles == 0 {
		l.MaxFiles = 100000
	}
	if l.MaxBytes == 0 {
		l.MaxBytes = 64 << 30
	}
	if l.MaxFiles < 0 || l.MaxBytes < 0 {
		return l, errors.New("invalid backup size limits")
	}
	return l, nil
}

type sourceFile struct {
	File
	relative string
	info     os.FileInfo
}
type contextReader struct {
	ctx   context.Context
	input io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}

func openSource(root *os.Root, name string, info os.FileInfo) (*os.File, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	current, err := file.Stat()
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
		file.Close()
		return nil, errors.New("backup source changed while it was being read")
	}
	return file, nil
}

func collect(ctx context.Context, root *os.Root, selections []Selection, options Options) ([]sourceFile, error) {
	files := []sourceFile{}
	var bytes int64
	report(options.Report, Progress{Phase: "inventory"})
	var walk func(string, string) error
	walk = func(relative, name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validPath(name) || !filepath.IsLocal(relative) {
			return errors.New("invalid backup source path")
		}
		if options.Exclude != nil && options.Exclude(name) {
			return nil
		}
		info, err := root.Lstat(relative)
		if err != nil {
			return err
		}
		if info.IsDir() {
			directory, err := root.Open(relative)
			if err != nil {
				return err
			}
			entries, err := directory.ReadDir(-1)
			directory.Close()
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if err = walk(filepath.Join(relative, entry.Name()), path.Join(name, entry.Name())); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("device backup cannot include links or special files")
		}
		if options.IncludeFile != nil && !options.IncludeFile(name) {
			return nil
		}
		if len(files) >= options.Limits.MaxFiles || info.Size() > options.Limits.MaxBytes-bytes {
			return errors.New("device backup exceeds its size limits")
		}
		file, err := openSource(root, relative, info)
		if err != nil {
			return err
		}
		digest := sha256.New()
		var read int64
		n, err := io.Copy(io.MultiWriter(digest, progressWriter{func(n int) {
			read += int64(n)
			report(options.Report, Progress{Phase: "inventory", Files: len(files), Bytes: bytes + read})
		}}), contextReader{ctx, file})
		file.Close()
		if err != nil {
			return err
		}
		if n != info.Size() {
			return errors.New("backup source size changed")
		}
		bytes += n
		files = append(files, sourceFile{File: File{Name: name, Bytes: n, SHA256: hex.EncodeToString(digest.Sum(nil))}, relative: relative, info: info})
		report(options.Report, Progress{Phase: "inventory", Files: len(files), Bytes: bytes})
		return nil
	}
	for _, selection := range selections {
		if err := walk(selection.Path, selection.Name); err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// Inventory fingerprints a prepared directory after domain-specific validation
// and migrations. A later adoption can reject edits made during user review.
func Inventory(ctx context.Context, directory string) ([]File, error) {
	return InventoryFiltered(ctx, directory, Options{})
}

func InventoryFiltered(ctx context.Context, directory string, options Options) ([]File, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := file.ReadDir(-1)
	file.Close()
	if err != nil {
		return nil, err
	}
	selections := make([]Selection, 0, len(entries))
	for _, entry := range entries {
		selections = append(selections, Selection{Path: entry.Name(), Name: entry.Name()})
	}
	limits, err := options.Limits.normalized()
	if err != nil {
		return nil, err
	}
	options.Limits = limits
	values, err := collect(ctx, root, selections, options)
	if err != nil {
		return nil, err
	}
	result := make([]File, len(values))
	for i, value := range values {
		result[i] = value.File
	}
	if err = validate(Manifest{Format: 1, Kind: "mewdex-device", Files: result}, limits); err != nil {
		return nil, err
	}
	return result, nil
}

func Fingerprint(ctx context.Context, directory string, selections []Selection) ([]File, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	limits, _ := (Limits{}).normalized()
	values, err := collect(ctx, root, selections, Options{Limits: limits})
	if err != nil {
		return nil, err
	}
	result := make([]File, len(values))
	for i, value := range values {
		result[i] = value.File
	}
	return result, nil
}

// Create never replaces an existing destination, including a symbolic link.
// An interrupted write cannot pass age authentication or the full manifest check.
func Create(ctx context.Context, directory, destination, password string, metadata json.RawMessage, selections []Selection, options Options) (Manifest, error) {
	var empty Manifest
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 {
		return empty, errors.New("use a backup passphrase of at least 12 characters")
	}
	if !json.Valid(metadata) {
		return empty, errors.New("invalid device backup metadata")
	}
	limits, err := options.Limits.normalized()
	if err != nil {
		return empty, err
	}
	options.Limits = limits
	root, err := os.OpenRoot(directory)
	if err != nil {
		return empty, err
	}
	defer root.Close()
	sources, err := collect(ctx, root, selections, options)
	if err != nil {
		return empty, err
	}
	manifest := Manifest{Format: 1, Kind: "mewdex-device", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Metadata: metadata, Files: make([]File, len(sources))}
	for i, source := range sources {
		manifest.Files[i] = source.File
	}
	if err = validate(manifest, limits); err != nil {
		return empty, err
	}
	raw, err := json.Marshal(manifest)
	if err != nil || len(raw) > maxManifestBytes {
		return empty, errors.New("device backup manifest is too large")
	}
	parent, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return empty, err
	}
	defer parent.Close()
	name := filepath.Base(destination)
	output, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return empty, err
	}
	complete := false
	defer func() {
		output.Close()
		if !complete {
			_ = parent.Remove(name)
		}
	}()
	recipient, err := age.NewScryptRecipient(password)
	if err != nil {
		return empty, err
	}
	recipient.SetWorkFactor(passwordWorkFactor)
	report(options.Report, Progress{Phase: "encrypting"})
	encrypted, err := age.Encrypt(output, recipient)
	if err != nil {
		return empty, err
	}
	writer := tar.NewWriter(encrypted)
	if err = writer.WriteHeader(&tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(raw))}); err != nil {
		return empty, err
	}
	if _, err = writer.Write(raw); err != nil {
		return empty, err
	}
	progress := Progress{Phase: "writing", TotalFiles: len(sources)}
	for _, source := range sources {
		progress.TotalBytes += source.Bytes
	}
	report(options.Report, progress)
	for _, source := range sources {
		if err = writer.WriteHeader(&tar.Header{Name: source.Name, Typeflag: tar.TypeReg, Mode: 0600, Size: source.Bytes}); err != nil {
			return empty, err
		}
		input, err := openSource(root, source.relative, source.info)
		if err != nil {
			return empty, err
		}
		digest := sha256.New()
		n, err := io.Copy(io.MultiWriter(writer, digest, progressWriter{func(n int) {
			progress.Bytes += int64(n)
			report(options.Report, progress)
		}}), contextReader{ctx, input})
		input.Close()
		if err != nil {
			return empty, err
		}
		if n != source.Bytes || hex.EncodeToString(digest.Sum(nil)) != source.SHA256 {
			return empty, errors.New("backup source changed during encryption")
		}
		progress.Files++
		report(options.Report, progress)
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	report(options.Report, Progress{Phase: "verifying", Files: progress.Files, TotalFiles: progress.TotalFiles, Bytes: progress.Bytes, TotalBytes: progress.TotalBytes})
	if err = writer.Close(); err != nil {
		return empty, err
	}
	if err = encrypted.Close(); err != nil {
		return empty, err
	}
	if err = output.Sync(); err != nil {
		return empty, err
	}
	if err = output.Close(); err != nil {
		return empty, err
	}
	complete = true
	return manifest, nil
}

// Extract writes only to a newly created child of parent. On any failure it
// removes that owned child through os.Root; existing state is never replaced.
// The caller must validate domain metadata before adopting the restored state.
func Extract(ctx context.Context, archive, password, parent, name string, limits Limits) (Manifest, error) {
	input, err := os.Open(archive)
	if err != nil {
		return Manifest{}, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return Manifest{}, err
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, errors.New("backup must be a regular encrypted file")
	}
	return ExtractReader(ctx, input, password, parent, name, limits)
}

// ExtractReader allows callers to fingerprint the exact authenticated ciphertext
// as it is consumed, without reopening a path that may have been replaced.
func ExtractReader(ctx context.Context, input io.Reader, password, parent, name string, limits Limits) (Manifest, error) {
	return ExtractReaderWithOptions(ctx, input, password, parent, name, Options{Limits: limits})
}

func ExtractReaderWithOptions(ctx context.Context, input io.Reader, password, parent, name string, options Options) (Manifest, error) {
	var empty Manifest
	if !validPath(name) || strings.Contains(name, "/") {
		return empty, errors.New("invalid restore directory name")
	}
	limits, err := options.Limits.normalized()
	if err != nil {
		return empty, err
	}
	identity, err := age.NewScryptIdentity(password)
	if err != nil {
		return empty, err
	}
	identity.SetMaxWorkFactor(passwordWorkFactor)
	report(options.Report, Progress{Phase: "unlocking"})
	plain, err := age.Decrypt(contextReader{ctx, input}, identity)
	if err != nil {
		return empty, errors.New("backup could not be unlocked with this passphrase")
	}
	reader := tar.NewReader(contextReader{ctx, plain})
	header, err := reader.Next()
	if err != nil || header.Name != "manifest.json" || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > maxManifestBytes {
		return empty, errors.New("invalid device backup header")
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return empty, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil {
		return empty, errors.New("invalid device backup manifest")
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return empty, errors.New("extra device backup metadata")
	}
	if err = validate(manifest, limits); err != nil {
		return empty, err
	}
	container, err := os.OpenRoot(parent)
	if err != nil {
		return empty, err
	}
	defer container.Close()
	if err = container.Mkdir(name, 0700); err != nil {
		return empty, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = container.RemoveAll(name)
		}
	}()
	root, err := container.OpenRoot(name)
	if err != nil {
		return empty, err
	}
	defer root.Close()
	progress := Progress{Phase: "extracting", TotalFiles: len(manifest.Files)}
	for _, entry := range manifest.Files {
		progress.TotalBytes += entry.Bytes
	}
	report(options.Report, progress)
	for _, entry := range manifest.Files {
		header, err = reader.Next()
		if err != nil || header.Typeflag != tar.TypeReg || header.Name != entry.Name || header.Size != entry.Bytes {
			return empty, errors.New("backup contents do not match its manifest")
		}
		if err = root.MkdirAll(filepath.FromSlash(path.Dir(entry.Name)), 0700); err != nil {
			return empty, err
		}
		output, err := root.OpenFile(filepath.FromSlash(entry.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return empty, err
		}
		digest := sha256.New()
		n, err := io.Copy(io.MultiWriter(output, digest, progressWriter{func(n int) {
			progress.Bytes += int64(n)
			report(options.Report, progress)
		}}), reader)
		if err == nil {
			err = output.Sync()
		}
		closeErr := output.Close()
		if err != nil {
			return empty, err
		}
		if closeErr != nil {
			return empty, closeErr
		}
		if n != entry.Bytes || hex.EncodeToString(digest.Sum(nil)) != entry.SHA256 {
			return empty, errors.New("restored backup failed its integrity check")
		}
		progress.Files++
		report(options.Report, progress)
	}
	if _, err = reader.Next(); err != io.EOF {
		return empty, errors.New("backup has undeclared or invalid contents")
	}
	// tar EOF is not the encrypted stream's EOF. Drain and verify the final age
	// authentication tag, accepting only bounded zero padding after the tar end.
	var padding int
	progress.Phase = "verifying"
	report(options.Report, progress)
	buffer := make([]byte, 4096)
	for {
		n, readErr := plain.Read(buffer)
		padding += n
		if padding > 1<<20 {
			return empty, errors.New("oversized backup padding")
		}
		for _, value := range buffer[:n] {
			if value != 0 {
				return empty, errors.New("backup has data after the archive end")
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return empty, errors.New("backup encryption was truncated or modified")
		}
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	complete = true
	return manifest, nil
}
