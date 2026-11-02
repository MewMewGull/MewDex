package lifecycle

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const OwnershipPending = "service-ownership.pending"

var ErrUnconfirmed = errors.New("service descendant cleanup is unconfirmed")

func (l *Lock) Directory() string { return filepath.Dir(l.file.Name()) }

// BeginOwnership leaves a durable refusal to adopt an old service lifetime.
// Its contents are a correlation ID, never a PID list to replay after restart.
func BeginOwnership(leases []*Lock, id string) error {
	if len(id) < 16 || len(id) > 128 {
		return errors.New("invalid service ownership identifier")
	}
	origin, err := newOwnershipOrigin(id)
	if err != nil {
		return err
	}
	created := []string{}
	for _, lease := range leases {
		dir := lease.Directory()
		if err := createOwnershipOrigin(dir, origin); err != nil {
			return errors.Join(err, CompleteOwnership(created, id))
		}
		file, err := os.OpenFile(filepath.Join(dir, OwnershipPending), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_, writeErr := io.WriteString(file, id)
			err = errors.Join(writeErr, file.Sync(), file.Close())
			created = append(created, dir)
		}
		if err != nil {
			// No protected child has been allowed to start at this point.
			return errors.Join(err, CompleteOwnership(created, id))
		}
	}
	return nil
}

// CompleteOwnership requires the current lifetime's exact marker. Absence or
// changed contents cannot be treated as successful cleanup of another owner.
func CompleteOwnership(directories []string, id string) error {
	var result error
	for _, dir := range directories {
		path := filepath.Join(dir, OwnershipPending)
		file, err := os.Open(path)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, 129))
		err = errors.Join(readErr, file.Close())
		if err == nil && string(raw) != id {
			err = ErrUnconfirmed
		}
		if err == nil {
			err = os.Remove(path)
		}
		result = errors.Join(result, err)
	}
	return result
}

func confirmOwnership(dir string) error {
	if _, err := os.Lstat(filepath.Join(dir, OwnershipPending)); err == nil {
		return fmt.Errorf("review the previous service cleanup before using this directory: %w", ErrUnconfirmed)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
