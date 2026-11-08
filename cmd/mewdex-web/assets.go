package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"mewdex/internal/protocol"
)

func uiDigest(directory string) (string, error) {
	digest := sha256.New()
	err := fs.WalkDir(os.DirFS(directory), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("web assets must be regular files")
		}
		file, err := os.Open(filepath.Join(directory, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		defer file.Close()
		_, _ = digest.Write(append([]byte(name), 0))
		fileHash := sha256.New()
		if _, err = io.Copy(fileHash, file); err != nil {
			return err
		}
		_, _ = digest.Write(fileHash.Sum(nil))
		return nil
	})
	return hex.EncodeToString(digest.Sum(nil)), err
}

func stageUI(root, source string) (string, error) {
	digest, err := uiDigest(source)
	if err != nil {
		return "", err
	}
	base := filepath.Join(root, "web-assets")
	destination := filepath.Join(base, digest)
	if current, readErr := uiDigest(destination); readErr == nil {
		if current != digest {
			return "", errors.New("the staged web assets changed")
		}
		return destination, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", readErr
	}
	if err = os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	stage := filepath.Join(base, ".stage-"+protocol.ID())
	if err = os.CopyFS(stage, os.DirFS(source)); err != nil {
		return "", err
	}
	// Rename only complete, verified assets. An interrupted copy stays separate.
	if current, checkErr := uiDigest(stage); checkErr != nil || current != digest {
		return "", errors.New("web assets changed during staging")
	}
	if err = os.Rename(stage, destination); err != nil {
		return "", err
	}
	return destination, nil
}
