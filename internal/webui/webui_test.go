package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWebUIIndexAssetsAndMissingFiles(t *testing.T) {
	files := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><html><head></head><body>workbench</body></html>")},
		"assets/app-abc123.js": {Data: []byte("console.log('workbench')")},
		".keep":                {},
	}
	h := handler(files)
	for _, path := range []string{"/", "/index.html"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 || !strings.Contains(r.Body.String(), `name="mewdex-hub" content="same-origin"`) || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid index response: %d %s", r.Code, r.Body.String())
		}
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/assets/app-abc123.js", nil))
	if r.Code != 200 || !strings.Contains(r.Header().Get("Content-Type"), "javascript") || !strings.Contains(r.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("bundled asset did not retain its type and cache policy", r.Result())
	}
	for _, path := range []string{"/.keep", "/assets/", "/assets/missing.js", "/device.json", "/.local/client-auth.json", "/v1/unknown"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != http.StatusNotFound {
			t.Fatalf("unexpected response for %s: %d", path, r.Code)
		}
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("HEAD", "/", nil))
	if r.Code != 200 || r.Body.Len() != 0 {
		t.Fatal("HEAD must omit the document body")
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("POST", "/", nil))
	if r.Code != http.StatusMethodNotAllowed {
		t.Fatal("web assets accepted a mutation")
	}
}

func TestMissingWebBuildIsExplicit(t *testing.T) {
	r := httptest.NewRecorder()
	handler(fstest.MapFS{}).ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != http.StatusServiceUnavailable {
		t.Fatal("missing build was served as a workbench")
	}
}

func TestWebWorkbenchIsOptInAndInvalidSettingsDisableIt(t *testing.T) {
	root := t.TempDir()
	if Enabled(root) {
		t.Fatal("a new installation exposed the web workbench")
	}
	for _, input := range []struct {
		raw     string
		enabled bool
	}{
		{`{"enabled":true}`, true},
		{`{"enabled":false}`, false},
		{`{"enabled":"true"}`, false},
		{`invalid JSON`, false},
	} {
		if err := os.WriteFile(filepath.Join(root, SettingsFile), []byte(input.raw), 0600); err != nil {
			t.Fatal(err)
		}
		if Enabled(root) != input.enabled {
			t.Fatalf("unexpected web state for %s", input.raw)
		}
	}
}
