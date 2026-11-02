package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// CopyVerified stages selected owned state without an archive. Callers hold the
// source's lifecycle lock and own the destination; existing files are preserved.
func CopyVerified(ctx context.Context, directory, destination string, selections []Selection, options Options) ([]File, error) {
	limits, err := options.Limits.normalized()
	if err != nil {
		return nil, err
	}
	options.Limits = limits
	input, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	sources, err := collect(ctx, input, selections, options)
	if err != nil {
		return nil, err
	}
	files := make([]File, len(sources))
	var total, copied int64
	for i, source := range sources {
		files[i] = source.File
		total += source.Bytes
	}
	if err = validate(Manifest{Format: 1, Kind: "mewdex-device", Files: files}, limits); err != nil {
		return nil, err
	}
	output, err := os.OpenRoot(destination)
	if err != nil {
		return nil, err
	}
	defer output.Close()
	for i, source := range sources {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		file, err := openSource(input, source.relative, source.info)
		if err != nil {
			return nil, err
		}
		target := filepath.FromSlash(source.Name)
		if err = output.MkdirAll(filepath.Dir(target), 0700); err != nil {
			file.Close()
			return nil, err
		}
		dest, err := output.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			file.Close()
			return nil, err
		}
		hash := sha256.New()
		count, copyErr := io.Copy(io.MultiWriter(dest, hash, progressWriter{func(n int) {
			copied += int64(n)
			report(options.Report, Progress{Phase: "copying", Files: i, TotalFiles: len(files), Bytes: copied, TotalBytes: total})
		}}), contextReader{ctx, file})
		file.Close()
		if copyErr == nil && (count != source.Bytes || hex.EncodeToString(hash.Sum(nil)) != source.SHA256) {
			copyErr = errors.New("source state changed while preparing its copy")
		}
		if copyErr == nil {
			copyErr = dest.Sync()
		}
		closeErr := dest.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return files, nil
}
