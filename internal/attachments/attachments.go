package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"mewdex/internal/protocol"
)

const MaxSize int64 = 20 << 20

var validID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Store struct{ dir string }

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}
func (s *Store) Path(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", errors.New("invalid attachment ID")
	}
	return filepath.Join(s.dir, id), nil
}
func (s *Store) Metadata(id string) (protocol.Attachment, error) {
	var a protocol.Attachment
	p, err := s.Path(id)
	if err != nil {
		return a, err
	}
	b, err := os.ReadFile(p + ".json")
	if err != nil {
		return a, err
	}
	err = json.Unmarshal(b, &a)
	if err == nil && a.ID != id {
		err = errors.New("attachment metadata ID mismatch")
	}
	return a, err
}

// MetadataForTarget is also checked on the executor's cache. Transport
// authorization alone cannot bind an already cached attachment to a task.
func (s *Store) MetadataForTarget(id, device, project string) (protocol.Attachment, error) {
	meta, err := s.Metadata(id)
	if err != nil {
		return meta, err
	}
	if device == "" || project == "" || meta.TargetDeviceID != device || meta.TargetProjectID != project {
		return protocol.Attachment{}, errors.New("attachment does not belong to this execution device and project")
	}
	return meta, nil
}
func (s *Store) Open(id string) (*os.File, error) {
	p, err := s.Path(id)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (s *Store) Save(name string, r io.Reader) (protocol.Attachment, error) {
	return s.SaveTarget(name, r, "", "", "")
}
func (s *Store) SaveTarget(name string, r io.Reader, owner, device, project string) (protocol.Attachment, error) {
	return s.Import(protocol.Attachment{ID: protocol.ID(), Name: filepath.Base(name), CreatedAt: protocol.Now(), OwnerID: owner, TargetDeviceID: device, TargetProjectID: project}, r)
}
func (s *Store) Import(meta protocol.Attachment, r io.Reader) (protocol.Attachment, error) {
	p, err := s.Path(meta.ID)
	if err != nil {
		return meta, err
	}
	tmp, err := os.CreateTemp(s.dir, "upload-*")
	if err != nil {
		return meta, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	count, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(r, MaxSize+1))
	if err != nil {
		return meta, err
	}
	if count > MaxSize {
		return meta, errors.New("attachment exceeds 20 MiB")
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if meta.SHA256 != "" && (meta.SHA256 != sum || meta.Size != count) {
		return meta, errors.New("attachment integrity check failed")
	}
	meta.SHA256 = sum
	meta.Size = count
	if _, err = tmp.Seek(0, 0); err != nil {
		return meta, err
	}
	header := make([]byte, 512)
	n, _ := tmp.Read(header)
	meta.MediaType = http.DetectContentType(header[:n])
	if err = tmp.Close(); err != nil {
		return meta, err
	}
	if old, e := s.Metadata(meta.ID); e == nil {
		if old.SHA256 == sum && old.Size == count && old.OwnerID == meta.OwnerID && old.TargetDeviceID == meta.TargetDeviceID && old.TargetProjectID == meta.TargetProjectID && old.Name == meta.Name {
			return old, nil
		}
		return meta, errors.New("attachment ID conflict")
	} else if !errors.Is(e, os.ErrNotExist) {
		return meta, e
	}
	// Link the completed file without replacing an existing uncommitted blob.
	// An interrupted import stays preserved instead of silently changing owner.
	if err = os.Link(tmp.Name(), p); err != nil {
		return meta, err
	}
	if err = os.WriteFile(p+".json", protocol.JSON(meta), 0600); err != nil {
		return meta, err
	}
	return meta, nil
}
