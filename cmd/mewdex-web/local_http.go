package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"

	"mewdex/internal/config"
)

//go:embed settings.html
var settingsHTML []byte

//go:embed settings.js
var settingsJS []byte

func (h *webHost) localHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'unsafe-inline'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
	if r.URL.Path == "/__mewdex/channels" {
		h.channelHTTP(w, r)
		return
	}
	if r.URL.Path == "/__mewdex/session" && r.Method == "GET" {
		if !h.localKey(r) {
			http.Error(w, "open the web input from its tray", 403)
			return
		}
		connection := h.channels.Connection()
		w.Header().Set("Content-Type", "application/json")
		if connection.Token == "" {
			_, _ = w.Write([]byte("null"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"baseURL": "http://" + r.Host, "credentials": map[string]string{"token": connection.Token, "hubDeviceId": connection.HubDeviceID, "identityId": connection.IdentityID}})
		return
	}
	if r.Method == "GET" && r.URL.Path == "/__mewdex/settings" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(settingsHTML)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/__mewdex/settings.js" {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(settingsJS)
		return
	}
	if r.URL.Path != "/__mewdex/config" {
		http.NotFound(w, r)
		return
	}
	if !h.localKey(r) {
		http.Error(w, "请从网页输入端托盘打开连接设置。", 403)
		return
	}
	if r.Method == "GET" {
		h.mu.RLock()
		target := h.settings.HubURL
		h.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"hubURL": target, "startup": startupEnabled(h.root)})
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	h.management.Lock()
	defer h.management.Unlock()
	var request struct {
		HubURL string `json:"hubURL"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		http.Error(w, "invalid connection settings", 400)
		return
	}
	request.HubURL = strings.TrimRight(strings.TrimSpace(request.HubURL), "/")
	h.mu.RLock()
	settings := h.settings
	h.mu.RUnlock()
	if err := config.ValidateURL(request.HubURL, settings.AllowPrivateHTTP); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if request.HubURL != settings.HubURL && h.channels.Connected() {
		http.Error(w, "请先在当前输入端退出账号，再切换 Hub。", 409)
		return
	}
	settings.HubURL = request.HubURL
	if err := saveSettings(h.root, settings); err != nil {
		http.Error(w, "无法保存连接设置。", 500)
		return
	}
	h.mu.Lock()
	h.settings = settings
	h.mu.Unlock()
	if startupEnabled(h.root) {
		if err := h.applyStartup(r.Context(), true); err != nil {
			http.Error(w, "连接已保存，请在托盘重新启用自启动。", 409)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
