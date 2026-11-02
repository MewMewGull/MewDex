package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

func TestDeviceAttachmentCredentialDoesNotFollowARedirect(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	dir := t.TempDir()
	token := filepath.Join(dir, "node-token")
	if err := os.WriteFile(token, []byte("temporary-device-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	fetch := AttachmentFetcher(config.Config{HubURL: redirect.URL, NodeTokenFile: token, DataDir: dir})
	if err := fetch(context.Background(), []protocol.Input{{Type: "file", AttachmentID: protocol.ID()}}); err == nil {
		t.Fatal("device followed a Hub redirect")
	}
	if reached.Load() != 0 {
		t.Fatal("credentialed request reached the redirect target")
	}
}
