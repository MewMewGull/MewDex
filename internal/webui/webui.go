// Package webui serves the same built input UI embedded in the desktop app.
package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:static
var bundled embed.FS

type WebWorkbenchSettings struct {
	Enabled bool `json:"enabled"`
}

const SettingsFile = "web-workbench.json"

func Enabled(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, SettingsFile))
	var settings WebWorkbenchSettings
	return err == nil && json.Unmarshal(raw, &settings) == nil && settings.Enabled
}

func Handler() http.Handler {
	files, _ := fs.Sub(bundled, "static")
	return handler(files)
}

func Available() bool {
	_, err := bundled.ReadFile("static/index.html")
	return err == nil
}

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
				http.Error(w, "此版本未包含网页界面，请安装完整的 MewDex 应用。", http.StatusServiceUnavailable)
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
