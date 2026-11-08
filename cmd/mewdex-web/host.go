package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/inputbridge"
	"mewdex/internal/lifecycle"
	"mewdex/internal/localnode"
	"mewdex/internal/protocol"
	"mewdex/internal/servicetray"
	"mewdex/internal/webinput"
	"mewdex/internal/webui"
)

type webHost struct {
	root, address, key string
	mu                 sync.RWMutex
	settings           webSettings
	channels           *webinput.Channels
	stop               context.CancelFunc
	management         sync.Mutex
	channelEpoch       map[string]uint64
}

func runWeb(args []string, managed bool) error {
	program, err := os.Executable()
	if err != nil {
		return err
	}
	userDirectory, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("MewDex Web", flag.ContinueOnError)
	rootFlag := flags.String("root", filepath.Join(userDirectory, "MewDex", "web"), "web input configuration directory")
	listen := flags.String("listen", "", "web input listener")
	directory := flags.String("ui", "", "standalone input assets")
	hubURL := flags.String("hub-url", "", "Hub HTTP(S) endpoint")
	private := flags.Bool("allow-private-http", false, "allow a private development Hub over HTTP")
	open := flags.Bool("open", len(args) == 0, "open the browser")
	noTray := flags.Bool("no-tray", false, "run without an interactive desktop tray")
	owner := flags.String("owner", "", "startup registration owner")
	if err = flags.Parse(args); err != nil {
		return err
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	settings, err := readSettings(root)
	if errors.Is(err, os.ErrNotExist) && !managed {
		settings = webSettings{ID: protocol.ID(), Listen: "127.0.0.1:47831", Directory: filepath.Join(filepath.Dir(program), "ui")}
	} else if err != nil {
		return err
	}
	before := settings
	if !managed && *directory == "" {
		bundled := filepath.Join(filepath.Dir(program), "ui")
		if info, statErr := os.Stat(filepath.Join(bundled, "index.html")); statErr == nil && info.Mode().IsRegular() {
			settings.Directory = bundled
		}
	}
	if *listen != "" {
		settings.Listen = *listen
	}
	if *directory != "" {
		settings.Directory, err = filepath.Abs(*directory)
		if err != nil {
			return err
		}
	}
	if *hubURL != "" {
		settings.HubURL = strings.TrimRight(*hubURL, "/")
	}
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "allow-private-http" {
			settings.AllowPrivateHTTP = *private
		}
	})
	if before.HubURL != settings.HubURL {
		if _, err = os.Stat(filepath.Join(root, "channel-connection")); err == nil {
			return errors.New("sign out the current web input account before switching its Hub")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if settings.HubURL != "" {
		if err = config.ValidateURL(settings.HubURL, settings.AllowPrivateHTTP); err != nil {
			return err
		}
	}
	if _, err = os.Stat(filepath.Join(settings.Directory, "index.html")); err != nil {
		return err
	}
	lease, err := lifecycle.Installation(root)
	if err != nil {
		if managed {
			return nil
		}
		var state struct {
			URL string `json:"url"`
			ID  string `json:"id"`
		}
		raw, readErr := os.ReadFile(filepath.Join(root, "web-server.json"))
		if !*open || readErr != nil || json.Unmarshal(raw, &state) != nil || state.ID != settings.ID || !strings.HasPrefix(state.URL, "http://127.0.0.1:") {
			return err
		}
		return servicetray.OpenURL(state.URL)
	}
	defer lease.Close()
	controller, controllerErr := webStartup(root)
	if managed {
		if before != settings {
			return errors.New("managed web input settings must match their startup registration")
		}
		if controllerErr != nil {
			return controllerErr
		}
		binding, bindErr := webBinding(root)
		if bindErr != nil {
			return bindErr
		}
		authorized, authErr := controller.Authorized(*owner, program, binding)
		if authErr != nil || !authorized {
			return authErr
		}
	}
	log, err := localnode.OpenServiceLog(root)
	if err != nil {
		return err
	}
	beforeOut, beforeErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = log, log
	defer func() { os.Stdout, os.Stderr = beforeOut, beforeErr; log.Close() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = saveSettings(root, settings); err != nil {
		return err
	}
	channels, err := webinput.New(ctx, root)
	if err != nil {
		return err
	}
	defer channels.Close()
	listener, err := net.Listen("tcp", settings.Listen)
	if err != nil {
		return fmt.Errorf("无法使用网页监听地址 %s。请检查已运行的网页输入端托盘，或在退出旧服务后启动此版本。\n%w", settings.Listen, err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	address := "http://127.0.0.1:" + port
	keyPath := filepath.Join(root, "web-panel-key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		key = []byte(protocol.ID() + protocol.ID())
		err = os.WriteFile(keyPath, key, 0600)
	}
	if err != nil {
		return err
	}
	host := &webHost{root: root, address: address, key: string(key), settings: settings, channels: channels, stop: stop}
	if !managed && startupEnabled(root) {
		if err = host.applyStartup(ctx, true); err != nil {
			return err
		}
		if controllerErr == nil {
			binding, _ := webBinding(root)
			if err = controller.Start(ctx, binding); err != nil {
				return err
			}
		}
	}
	if !*noTray {
		closeTray, trayErr := servicetray.Start(ctx, host.tray())
		if trayErr != nil {
			return trayErr
		}
		defer closeTray()
	}
	statePath := filepath.Join(root, "web-server.json")
	if err = os.WriteFile(statePath, protocol.JSON(map[string]any{"id": settings.ID, "url": address, "pid": os.Getpid()}), 0600); err != nil {
		return err
	}
	defer os.Remove(statePath)
	server := &http.Server{Handler: host, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Fprintln(log, "MewDex web input ready", address)
	if *open {
		go func() { _ = host.open() }()
	}
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (h *webHost) open() error {
	h.mu.RLock()
	configured := h.settings.HubURL != ""
	h.mu.RUnlock()
	if !configured {
		return servicetray.OpenURL(h.address + "/__mewdex/settings#" + h.key)
	}
	return servicetray.OpenURL(h.address + "#mewdex-input=" + h.key)
}

func (h *webHost) tray() servicetray.Options {
	return servicetray.Options{Role: servicetray.InputRole, Title: "MewDex · 网页输入端", Menu: func() []servicetray.Item {
		autostart := servicetray.Item{ID: "startup.enable", Label: "开机自启动", Question: "启用网页版输入服务的开机自启动？"}
		if startupEnabled(h.root) {
			autostart.ID, autostart.Checked, autostart.Question = "startup.disable", true, "关闭网页版输入服务的开机自启动？"
		}
		return []servicetray.Item{{ID: "open", Label: "打开 MewDex 网页版"}, {ID: "settings", Label: "连接设置"}, {ID: "status", Label: "查看状态"}, {Separator: true}, autostart, {Separator: true}, {ID: "quit", Label: "退出网页输入端", Question: "退出后，此输入端的消息渠道连接将停止。是否继续？"}}
	}, Action: func(ctx context.Context, id string) (string, error) {
		h.management.Lock()
		defer h.management.Unlock()
		switch id {
		case "open":
			return "", h.open()
		case "settings":
			return "", servicetray.OpenURL(h.address + "/__mewdex/settings#" + h.key)
		case "status":
			h.mu.RLock()
			target := h.settings.HubURL
			h.mu.RUnlock()
			return "组件：网页输入端\n浏览器入口：" + h.address + "\n连接 Hub：" + target, nil
		case "startup.enable", "startup.disable":
			return "", h.applyStartup(ctx, id == "startup.enable")
		case "quit":
			controller, err := webStartup(h.root)
			if err != nil {
				return "", err
			}
			if err = controller.PauseRecovery(); err != nil {
				return "", err
			}
			h.stop()
			return "", nil
		}
		return "", errors.New("unknown web input action")
	}}
}

func (h *webHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/__mewdex/") {
		h.localHTTP(w, r)
		return
	}
	h.mu.RLock()
	settings := h.settings
	h.mu.RUnlock()
	if settings.HubURL == "" {
		http.Redirect(w, r, "/__mewdex/settings", http.StatusSeeOther)
		return
	}
	if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/v1/") {
		target, _ := url.Parse(settings.HubURL)
		newHubProxy(target).ServeHTTP(w, r)
		return
	}
	webui.HandlerForInput(settings.Directory, settings.HubURL).ServeHTTP(w, r)
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == r.Host && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func (h *webHost) channelHTTP(w http.ResponseWriter, r *http.Request) {
	h.management.Lock()
	defer h.management.Unlock()
	if r.Method != "POST" || !sameOrigin(r) {
		http.Error(w, "origin or method rejected", 403)
		return
	}
	var request struct {
		ClientID    string `json:"clientId"`
		Epoch       uint64 `json:"epoch"`
		Operation   string `json:"operation"`
		BaseURL     string `json:"baseURL"`
		HubDeviceID string `json:"hubDeviceId"`
		IdentityID  string `json:"identityId"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid connection request", 400)
		return
	}
	if request.ClientID == "" || len(request.ClientID) > 64 || request.Epoch == 0 {
		http.Error(w, "input request ordering required", 400)
		return
	}
	if h.channelEpoch == nil {
		h.channelEpoch = map[string]uint64{}
	}
	if request.Epoch < h.channelEpoch[request.ClientID] {
		http.Error(w, "stale input request", 409)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		http.Error(w, "account credential required", 401)
		return
	}
	var err error
	if request.Operation == "disconnect" {
		// A browser with another input identity can sign out without stopping this owner.
		connection := h.channels.Connection()
		if connection.Token == "" || subtle.ConstantTimeCompare([]byte(connection.Token), []byte(token)) == 1 {
			err = h.channels.Disconnect(token)
		}
	} else if request.Operation == "connect" {
		h.mu.RLock()
		target := h.settings.HubURL
		h.mu.RUnlock()
		if request.BaseURL != "http://"+r.Host && request.BaseURL != "https://"+r.Host && strings.TrimRight(request.BaseURL, "/") != target {
			http.Error(w, "use this web input's configured Hub", 400)
			return
		}
		name, _ := os.Hostname()
		err = h.channels.Configure(r.Context(), inputbridge.Options{BaseURL: target, Token: token, HubDeviceID: request.HubDeviceID, IdentityID: request.IdentityID, Name: name + " · Web"})
	} else {
		http.Error(w, "unsupported connection operation", 400)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	h.channelEpoch[request.ClientID] = request.Epoch
	w.Header().Set("Content-Type", "application/json")
	result := map[string]any{"ok": true}
	if request.Operation == "connect" && localBrowser(r) {
		result["settingsKey"] = h.key
	}
	_ = json.NewEncoder(w).Encode(result)
}

func localBrowser(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && sameOrigin(r)
}

func (h *webHost) localKey(r *http.Request) bool {
	return localBrowser(r) && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(h.key)) == 1
}
