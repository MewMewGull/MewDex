package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ownershipOrigin struct {
	Format      int    `json:"format"`
	ID          string `json:"id"`
	BootSession string `json:"bootSession,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

type Ownership struct {
	ID          string
	BootSession string
	Fingerprint string
	Legacy      bool
}

func ownershipDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func originPath(dir, id string) string {
	return filepath.Join(dir, "service-ownership."+ownershipDigest([]byte(id))+".json")
}

func readOwnershipFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrUnconfirmed
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && len(raw) > int(limit) {
		err = ErrUnconfirmed
	}
	return raw, err
}

// ReadOwnership is observational. Missing origin metadata remains legacy data;
// recovery must checkpoint a boot boundary before it can clear that marker.
func ReadOwnership(dir string) (*Ownership, error) {
	raw, err := readOwnershipFile(filepath.Join(dir, OwnershipPending), 128)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	id := string(raw)
	if len(id) < 16 || strings.TrimSpace(id) != id {
		return nil, ErrUnconfirmed
	}
	origin, err := readOwnershipFile(originPath(dir, id), 4096)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	result := &Ownership{ID: id, Legacy: origin == nil, Fingerprint: ownershipDigest(append(append([]byte{}, raw...), origin...))}
	if origin != nil {
		var record ownershipOrigin
		if json.Unmarshal(origin, &record) != nil || record.Format != 1 || record.ID != id {
			return nil, ErrUnconfirmed
		}
		if record.BootSession != "" {
			result.BootSession, err = NormalizeBootSession(record.BootSession)
			if err != nil {
				return nil, err
			}
		} else {
			result.Legacy = true
		}
	}
	return result, nil
}

func createOwnershipOrigin(dir string, record ownershipOrigin) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(originPath(dir, record.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(raw)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func newOwnershipOrigin(id string) (ownershipOrigin, error) {
	boot, err := BootSession()
	if err != nil && !errors.Is(err, ErrBootUnavailable) {
		return ownershipOrigin{}, err
	}
	return ownershipOrigin{Format: 1, ID: id, BootSession: boot, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, nil
}
