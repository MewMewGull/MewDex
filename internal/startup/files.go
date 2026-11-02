package startup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"mewdex/internal/protocol"
)

func readEntry(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("startup entry must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if len(raw) > 1<<20 {
		return nil, errors.New("startup entry exceeds its limit")
	}
	return raw, err
}

func writeEntry(path string, before, after []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	if before != nil {
		name += "." + protocol.ID() + ".tmp"
		defer root.Remove(name)
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(after)
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil || before == nil {
		return err
	}
	current, err := readEntry(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, current) {
		return errors.New("startup entry changed while saving; it was preserved")
	}
	return root.Rename(name, filepath.Base(path))
}

func removeEntry(path string, before []byte) error {
	current, err := readEntry(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(before, current) {
		return errors.New("startup entry changed before removal; it was preserved")
	}
	return os.Remove(path)
}
