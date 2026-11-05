package inputbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/messaging"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type Options struct {
	Root        string `json:"-"`
	BaseURL     string `json:"baseURL"`
	Token       string `json:"token"`
	HubDeviceID string `json:"hubDeviceId"`
	IdentityID  string `json:"identityId"`
}

func Run(ctx context.Context, options Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := config.ValidateURL(options.BaseURL, false); err != nil {
		return err
	}
	if options.Root == "" || options.Token == "" || options.HubDeviceID == "" || options.IdentityID == "" {
		return errors.New("a verified APP connection is required")
	}
	base := strings.TrimRight(options.BaseURL, "/")
	b := newBackend(ctx, base, options.Token, nil)
	// Check the public server identity before sending the APP's credential.
	request, _ := http.NewRequestWithContext(ctx, "GET", base+"/health", nil)
	response, err := b.client.Do(request)
	if err != nil {
		return errors.New("Hub identity check unavailable")
	}
	var server protocol.ServerInfo
	err = json.NewDecoder(http.MaxBytesReader(nil, response.Body, 64<<10)).Decode(&server)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || server.HubDeviceID != options.HubDeviceID || server.ProtocolVersion != protocol.Version {
		return errors.New("Hub identity changed")
	}
	var identity protocol.Identity
	if err = b.get("/v1/me", &identity); err != nil {
		return err
	}
	if identity.ID != options.IdentityID || identity.Role != "admin" {
		return errors.New("an administrator APP connection is required for channels")
	}
	digest := sha256.Sum256(protocol.JSON([]string{options.HubDeviceID, options.IdentityID}))
	directory := filepath.Join(options.Root, hex.EncodeToString(digest[:16]))
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	lease, err := lifecycle.Acquire(directory)
	if err != nil {
		return errors.New("APP channel connections are already running for this installation")
	}
	defer lease.Close()
	s, err := store.Open(filepath.Join(directory, "channels.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	b.store = s
	var bridgeID string
	if err = s.Get("input_bridge", "id", &bridgeID); errors.Is(err, store.ErrNotFound) {
		bridgeID = protocol.ID()
		err = s.Put("input_bridge", "id", bridgeID)
	}
	if err != nil {
		return err
	}
	manager, err := messaging.NewWithBackend(s, b, b.Submit)
	if err != nil {
		return err
	}
	manager.Start(ctx)
	defer func() { cancel(); manager.Close() }()
	name, _ := os.Hostname()
	if name == "" {
		name = "MewDex APP"
	}
	endpoint, _ := url.Parse(base + "/v1/input-bridges/ws")
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.RawQuery = url.Values{"id": {bridgeID}, "name": {name}}.Encode()
	for ctx.Err() == nil {
		conn, response, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{HTTPClient: b.client, HTTPHeader: http.Header{"Authorization": {"Bearer " + options.Token}}})
		if err == nil {
			manager.Reconnect()
			serveConnection(ctx, conn, manager, identity.ID)
			conn.CloseNow()
		} else if response != nil && (response.StatusCode == 401 || response.StatusCode == 403) {
			return errors.New("APP channel authorization was revoked")
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return nil
}

func serveConnection(ctx context.Context, conn *websocket.Conn, manager *messaging.Manager, owner string) {
	ctx, cancel := context.WithCancel(ctx)
	conn.SetReadLimit(128 << 10)
	var write sync.Mutex
	var workers sync.WaitGroup
	defer workers.Wait()
	slots := make(chan struct{}, 16)
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ping, stop := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(ping)
				stop()
				if err != nil {
					conn.CloseNow()
					return
				}
				manager.Reconnect()
			}
		}
	}()
	defer cancel()
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var request protocol.InputBridgeRequest
		if json.Unmarshal(raw, &request) != nil || request.ID == "" || request.Owner != owner {
			return
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-slots }()
			response := handle(ctx, manager, request)
			write.Lock()
			err := conn.Write(ctx, websocket.MessageText, protocol.JSON(response))
			write.Unlock()
			if err != nil {
				conn.CloseNow()
			}
		}()
	}
}

func handle(ctx context.Context, manager *messaging.Manager, request protocol.InputBridgeRequest) protocol.InputBridgeResponse {
	var value any
	var err error
	path := strings.TrimPrefix(request.Path, "/v1/messaging")
	switch {
	case request.Method == "GET" && path == "":
		value = manager.Snapshot()
	case request.Method == "POST" && path == "/weixin/login":
		value, err = manager.BeginWeixinLogin(ctx, request.Owner)
	case request.Method == "POST" && path == "/weixin/login/poll":
		value, err = manager.PollWeixinLogin(ctx, request.Owner)
	case request.Method == "PUT" && strings.Count(path, "/") == 1:
		var update protocol.MessagingUpdate
		err = json.Unmarshal(request.Body, &update)
		if err == nil {
			err = manager.Update(strings.TrimPrefix(path, "/"), update)
		}
		value = manager.Snapshot()
	default:
		err = errors.New("unknown APP channel operation")
	}
	status := 200
	if err != nil {
		status, value = 400, map[string]string{"error": err.Error()}
	}
	return protocol.InputBridgeResponse{ID: request.ID, Status: status, Body: protocol.JSON(value)}
}
