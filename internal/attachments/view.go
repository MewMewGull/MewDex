package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"

	"mewdex/internal/protocol"
)

// ViewTarget binds a readable input directory to exactly one local session.
// These are executor-owned identities, never a filesystem path from a client.
type ViewTarget struct {
	DeviceID  string `json:"deviceId"`
	ProjectID string `json:"projectId"`
	SessionID string `json:"sessionId"`
}

type View struct {
	dir    string
	target ViewTarget
	info   os.FileInfo
}

type viewMarker struct {
	Format  int    `json:"format"`
	Binding string `json:"binding"`
}

// NewView creates one derived input directory under an existing owned parent.
// Keep this directory outside all private roots passed to executionscope.
func NewView(dir string, target ViewTarget) (*View, error) {
	if !filepath.IsAbs(dir) || target.DeviceID == "" || target.ProjectID == "" || target.SessionID == "" {
		return nil, errors.New("an input view requires an absolute directory and a complete session target")
	}
	dir = filepath.Clean(dir)
	parent := filepath.Dir(dir)
	canonicalParent, err := filepath.EvalSymlinks(parent)
	if err != nil || canonicalParent != parent {
		return nil, errors.New("input view parent must be an existing canonical directory")
	}
	parentRoot, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer parentRoot.Close()
	created := true
	if err := parentRoot.Mkdir(filepath.Base(dir), 0700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		created = false
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil || canonical != filepath.Clean(dir) {
		return nil, errors.New("input view directory was redirected")
	}
	root, err := parentRoot.OpenRoot(filepath.Base(dir))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	marker := viewMarker{Format: 1, Binding: target.Key()}
	const markerName = ".mewdex-input-view.json"
	if created {
		file, err := root.OpenFile(markerName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		_, err = file.Write(protocol.JSON(marker))
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else {
		file, err := root.Open(markerName)
		if err != nil {
			return nil, errors.New("existing input view has no readable ownership marker; its files were preserved")
		}
		var prior viewMarker
		err = json.NewDecoder(io.LimitReader(file, 4096)).Decode(&prior)
		file.Close()
		if err != nil || prior != marker {
			return nil, errors.New("input view belongs to another session or has an invalid ownership marker")
		}
	}
	return &View{dir: canonical, target: target, info: info}, nil
}

func (v *View) Directory() string { return v.dir }

func (v *View) openRoot() (*os.Root, error) {
	canonical, err := filepath.EvalSymlinks(v.dir)
	if err != nil || canonical != v.dir {
		return nil, errors.New("input view directory moved or was redirected")
	}
	root, err := os.OpenRoot(v.dir)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil || !os.SameFile(info, v.info) {
		root.Close()
		return nil, errors.New("input view directory identity changed")
	}
	return root, nil
}

func verifyViewFile(file *os.File, meta protocol.Attachment) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != meta.Size || meta.Size < 0 || meta.Size > MaxSize {
		return errors.New("input view size or file type changed")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(file, MaxSize+1)); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != meta.SHA256 {
		return errors.New("input view content changed; its files were preserved")
	}
	return nil
}

// Stage copies only referenced, integrity-checked input. The original cache
// and its metadata stay private; the model receives only this session's view.
func (v *View) Stage(source *Store, id string) (string, error) {
	meta, err := source.MetadataForTarget(id, v.target.DeviceID, v.target.ProjectID)
	if err != nil {
		return "", err
	}
	if len(meta.SHA256) != 64 || meta.Size < 0 || meta.Size > MaxSize {
		return "", errors.New("invalid attachment integrity metadata")
	}
	root, err := v.openRoot()
	if err != nil {
		return "", err
	}
	defer root.Close()
	verifyExisting := func() error {
		file, err := root.Open(id)
		if err != nil {
			return err
		}
		defer file.Close()
		return verifyViewFile(file, meta)
	}
	if err = verifyExisting(); err == nil {
		return v.checkedPath(id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	cache, err := os.OpenRoot(source.dir)
	if err != nil {
		return "", err
	}
	defer cache.Close()
	input, err := cache.Open(id)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != meta.Size {
		return "", errors.New("original attachment size or file type changed")
	}
	return v.stageFile(root, id, meta, input)
}

// stageFile publishes a verified immutable file without replacing existing data.
func (v *View) stageFile(root *os.Root, id string, meta protocol.Attachment, input io.Reader) (string, error) {
	verifyExisting := func() error {
		file, err := root.Open(id)
		if err != nil {
			return err
		}
		defer file.Close()
		return verifyViewFile(file, meta)
	}
	if err := verifyExisting(); err == nil {
		return v.checkedPath(id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	temporary := "stage-" + protocol.ID()
	output, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer root.Remove(temporary)
	hash := sha256.New()
	count, err := io.Copy(io.MultiWriter(output, hash), io.LimitReader(input, MaxSize+1))
	if err == nil && (count != meta.Size || hex.EncodeToString(hash.Sum(nil)) != meta.SHA256) {
		err = errors.New("original attachment content changed")
	}
	if err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = root.Link(temporary, id); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		if err = verifyExisting(); err != nil {
			return "", err
		}
	}
	// A copy may finish after the directory was renamed. Never return a path
	// that no longer refers to this view, even though rooted writes stayed safe.
	return v.checkedPath(id)
}

// StageIndex rebuilds current paths from accepted attachment identities. It does
// not read old model-supplied paths or expose cache metadata for other inputs.
func (v *View) StageIndex(ctx context.Context, source *Store, ids []string) (string, error) {
	type entry struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		SHA256    string `json:"sha256"`
		Path      string `json:"path"`
	}
	index := struct {
		Format int        `json:"format"`
		Target ViewTarget `json:"target"`
		Files  []entry    `json:"files"`
	}{Format: 1, Target: v.target, Files: []entry{}}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if i > 0 && ids[i-1] == id {
			continue
		}
		meta, err := source.MetadataForTarget(id, v.target.DeviceID, v.target.ProjectID)
		if err != nil {
			return "", err
		}
		path, err := v.Stage(source, id)
		if err != nil {
			return "", err
		}
		root, err := v.openRoot()
		if err != nil {
			return "", err
		}
		file, err := root.Open(id)
		if err == nil {
			err = verifyViewFile(file, meta)
			file.Close()
		}
		root.Close()
		if err != nil {
			return "", err
		}
		index.Files = append(index.Files, entry{ID: id, Name: meta.Name, MediaType: meta.MediaType, Size: meta.Size, SHA256: meta.SHA256, Path: path})
	}
	raw := protocol.JSON(index)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if int64(len(raw)) > MaxSize {
		return "", errors.New("attachment history index exceeds its size limit")
	}
	digest := sha256.Sum256(raw)
	sum := hex.EncodeToString(digest[:])
	root, err := v.openRoot()
	if err != nil {
		return "", err
	}
	defer root.Close()
	return v.stageFile(root, "history-"+sum+".json", protocol.Attachment{Size: int64(len(raw)), SHA256: sum}, bytes.NewReader(raw))
}

func (v *View) checkedPath(id string) (string, error) {
	check, err := v.openRoot()
	if err != nil {
		return "", err
	}
	check.Close()
	return filepath.Join(v.dir, id), nil
}

func (t ViewTarget) Key() string {
	key := sha256.Sum256(protocol.JSON(t))
	return hex.EncodeToString(key[:])
}
