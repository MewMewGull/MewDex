// Package hostidentity identifies the OS installation independently of an app
// profile or service role. The underlying OS identifier stays local.
package hostidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
)

var cached struct {
	sync.Mutex
	id string
}

func ID(ctx context.Context) (string, error) {
	cached.Lock()
	defer cached.Unlock()
	if cached.id != "" {
		return cached.id, nil
	}
	raw, err := nativeID(ctx)
	if err != nil {
		return "", err
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if len(raw) < 16 || len(raw) > 256 {
		return "", errors.New("the OS did not provide a stable host identity")
	}
	sum := sha256.Sum256([]byte("app.mewdex.host.v1\x00" + raw))
	cached.id = hex.EncodeToString(sum[:])
	return cached.id, nil
}

func Valid(id string) bool {
	if len(id) != sha256.Size*2 || id != strings.ToLower(id) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
