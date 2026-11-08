// Package executorpanel exposes the executor's local management API and UI.
package executorpanel

import (
	"bytes"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mewdex/internal/localnode"
)

//go:embed static/*
var assets embed.FS

func Handler(manager *localnode.Manager, token string) http.Handler {
	return HandlerForRole(manager, token, "executor")
}

func HandlerForRole(manager *localnode.Manager, token, role string) http.Handler {
	var mutation sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		if r.URL.Path == "/api/panel" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"role": role})
			return
		}
		if r.URL.Path == "/api/device" {
			if r.Method != "POST" {
				w.Header().Set("Allow", "POST")
				http.Error(w, "method not allowed", 405)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != r.Host {
					http.Error(w, "origin rejected", 403)
					return
				}
			}
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(provided)) != 1 {
				http.Error(w, "panel token required", 401)
				return
			}
			var request localnode.Request
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
				http.Error(w, "invalid device request", 400)
				return
			}
			switch request.Operation {
			case "status", "setup", "start", "stop", "projects", "startup.status", "startup.apply", "repair.executor", "computer.status", "computer.windows", "computer.approve", "computer.stop":
			default:
				http.Error(w, "unsupported executor panel operation", 400)
				return
			}
			if request.Setup != nil && (request.Setup.Gateway || request.Setup.Hub != (role == "hub") || request.Setup.Executor != (role != "hub")) {
				http.Error(w, "use the panel for this component role", 400)
				return
			}
			if role == "hub" && request.Operation != "status" && request.Operation != "setup" && request.Operation != "start" && request.Operation != "stop" && request.Operation != "startup.status" && request.Operation != "startup.apply" {
				http.Error(w, "unsupported Hub panel operation", 400)
				return
			}
			mutation.Lock()
			defer mutation.Unlock()
			current, err := manager.Handle(r.Context(), localnode.Request{Operation: "status"})
			if err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			if current.Snapshot.Configured && (current.Snapshot.Hub != (role == "hub") || role != "hub" && !current.Snapshot.Executor) {
				http.Error(w, "use a separate component profile", 409)
				return
			}
			if request.Operation != "status" && request.Operation != "setup" && request.ExpectedDeviceID == "" {
				http.Error(w, "refresh the executor identity first", 409)
				return
			}
			result, err := manager.Handle(r.Context(), request)
			response := localnode.Response{Version: 1, OK: err == nil, Result: &result}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if err != nil {
				response.Result = nil
				response.Error = err.Error()
				w.WriteHeader(400)
			}
			_ = json.NewEncoder(w).Encode(response)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", 405)
			return
		}
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		if path != "/index.html" && path != "/panel.js" && path != "/panel.css" {
			http.NotFound(w, r)
			return
		}
		content, err := assets.ReadFile("static" + path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(content))
	})
}
