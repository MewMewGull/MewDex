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
	Name        string `json:"name,omitempty"`
	Standalone  bool   `json:"-"`
}

func Run(ctx context.Context, options Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	b, identity, err := verifiedBackend(ctx, options)
	if err != nil {
		return err
	}
	base := strings.TrimRight(options.BaseURL, "/")
	digest := sha256.Sum256(protocol.JSON([]string{options.HubDeviceID, options.IdentityID}))
	directory := filepath.Join(options.Root, hex.EncodeToString(digest[:16]))
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	lease, err := lifecycle.Acquire(directory)
	if err != nil {
		return errors.New("Message channel connections are already running for this installation")
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
	name := options.Name
	if name == "" {
		name, _ = os.Hostname()
		name += " · APP"
	}
	endpoint, _ := url.Parse(base + "/v1/input-bridges/ws")
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.RawQuery = url.Values{"id": {bridgeID}, "name": {name}}.Encode()
	if options.Standalone {
		query := endpoint.Query()
		query.Set("kind", "standalone")
		endpoint.RawQuery = query.Encode()
	}
	for ctx.Err() == nil {
		conn, response, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{HTTPClient: b.client, HTTPHeader: http.Header{"Authorization": {"Bearer " + options.Token}}})
		if err == nil {
			manager.Reconnect()
			serveConnection(ctx, conn, manager, identity.ID, options.Standalone)
			conn.CloseNow()
		} else if response != nil && (response.StatusCode == 401 || response.StatusCode == 403) {
			return errors.New("Input channel authorization was revoked")
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

// ValidateConnection verifies the public Hub before sending credentials.
func ValidateConnection(ctx context.Context, options Options) error {
	_, _, err := verifiedBackend(ctx, options)
	return err
}

func verifiedBackend(ctx context.Context, options Options) (*backend, protocol.Identity, error) {
	if err := config.ValidateURL(options.BaseURL, false); err != nil {
		return nil, protocol.Identity{}, err
	}
	if options.Root == "" || options.Token == "" || options.HubDeviceID == "" || options.IdentityID == "" {
		return nil, protocol.Identity{}, errors.New("a verified input connection is required")
	}
	base := strings.TrimRight(options.BaseURL, "/")
	b := newBackend(ctx, base, options.Token, nil)
	// Check the public server identity before sending the input's credential.
	request, _ := http.NewRequestWithContext(ctx, "GET", base+"/health", nil)
	response, err := b.client.Do(request)
	if err != nil {
		return nil, protocol.Identity{}, messaging.ErrHubUnavailable
	}
	var server protocol.ServerInfo
	err = json.NewDecoder(http.MaxBytesReader(nil, response.Body, 64<<10)).Decode(&server)
	response.Body.Close()
	if response.StatusCode >= 500 || response.StatusCode == 429 {
		return nil, protocol.Identity{}, messaging.ErrHubUnavailable
	}
	if err != nil || response.StatusCode != 200 || server.HubDeviceID != options.HubDeviceID || server.ProtocolVersion != protocol.Version {
		return nil, protocol.Identity{}, errors.New("Hub identity changed")
	}
	var identity protocol.Identity
	if err = b.get("/v1/me", &identity); err != nil {
		return nil, protocol.Identity{}, err
	}
	if identity.ID != options.IdentityID || identity.Role != "admin" {
		return nil, protocol.Identity{}, errors.New("an administrator input connection is required for channels")
	}
	return b, identity, nil
}

func serveConnection(ctx context.Context, conn *websocket.Conn, manager *messaging.Manager, owner string, standalone bool) {
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
		if json.Unmarshal(raw, &request) != nil || request.ID == "" || request.Owner == "" || !standalone && request.Owner != owner {
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
			response := manager.Handle(ctx, request)
			write.Lock()
			err := conn.Write(ctx, websocket.MessageText, protocol.JSON(response))
			write.Unlock()
			if err != nil {
				conn.CloseNow()
			}
		}()
	}
}
