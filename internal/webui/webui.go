// Package webui serves the separately deployed input terminal assets.
package webui

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type WebWorkbenchSettings struct {
	Enabled bool `json:"enabled"`
}

const SettingsFile = "web-workbench.json"

func Enabled(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, SettingsFile))
	var settings WebWorkbenchSettings
	return err == nil && json.Unmarshal(raw, &settings) == nil && settings.Enabled
}

func Handler(directory string) http.Handler { return handler(os.DirFS(directory)) }

func handler(files fs.FS) http.Handler {
	assets := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' https: wss: http: ws:; frame-src 'self' blob:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			index, err := fs.ReadFile(files, "index.html")
			if err != nil {
				http.Error(w, "网页资源不可用，请检查独立网页端的 ui 目录。", http.StatusServiceUnavailable)
				return
			}
			index = bytes.Replace(index, []byte("</head>"), []byte(`<meta name="mewdex-hub" content="same-origin"></head>`), 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodHead {
				_, _ = w.Write(index)
			}
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		info, err := fs.Stat(files, name)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		assets.ServeHTTP(w, r)
	})
}
