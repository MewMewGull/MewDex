package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/attachments"
	"mewdex/internal/config"
	"mewdex/internal/hostidentity"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type Handler interface {
	Handle(context.Context, protocol.Command) protocol.CommandResult
}

// AttachmentFetcher moves only referenced uploads to the target device and verifies their hashes.
func AttachmentFetcher(c config.Config) func(context.Context, []protocol.Input) error {
	return func(ctx context.Context, input []protocol.Input) error {
		if err := config.ValidateURL(c.HubURL, c.AllowPrivateHTTP); err != nil {
			return err
		}
		assets, err := attachments.New(filepath.Join(c.DataDir, "attachments"))
		if err != nil {
			return err
		}
		for _, item := range input {
			if item.AttachmentID == "" {
				continue
			}
			if _, err = assets.Metadata(item.AttachmentID); err == nil {
				continue
			}
			b, err := os.ReadFile(c.NodeTokenFile)
			if err != nil {
				return errors.New("cannot read device credential")
			}
			get := func(suffix string) (*http.Response, error) {
				request, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.HubURL, "/")+"/v1/attachments/"+url.PathEscape(item.AttachmentID)+suffix, nil)
				if err != nil {
					return nil, err
				}
				request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
				return hubHTTP.Do(request)
			}
			response, err := get("")
			if err != nil {
				return err
			}
			var meta protocol.Attachment
			if response.StatusCode != 200 {
				response.Body.Close()
				return errors.New("attachment metadata unavailable")
			}
			err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&meta)
			response.Body.Close()
			if err != nil {
				return err
			}
			if meta.ID != item.AttachmentID {
				return errors.New("invalid attachment metadata")
			}
			response, err = get("/content")
			if err != nil {
				return err
			}
			if response.StatusCode != 200 {
				response.Body.Close()
				return errors.New("attachment content unavailable")
			}
			_, err = assets.Import(meta, response.Body)
			response.Body.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
}

type Node struct {
	Config   config.Config
	Store    *store.Store
	Device   protocol.Device
	Executor Handler
}

var errPairingRequired = errors.New("Hub rejected this executor credential. Pair the executor again.")

// Run reconnects the transport only. Commands with unknown outcomes are not resubmitted.
func (n *Node) Run(ctx context.Context) error {
	if err := config.ValidateURL(n.Config.HubURL, n.Config.AllowPrivateHTTP); err != nil {
		return err
	}
	if n.Device.HostID == "" {
		host, err := hostidentity.ID(ctx)
		if err != nil {
			return err
		}
		n.Device.HostID = host
	}
	instanceID := protocol.ID()
	delay := time.Second
	_ = n.Store.Put("status", "connection", protocol.NodeConnection{State: "connecting", At: protocol.Now()})
	for {
		err := n.connect(ctx, instanceID, func() { delay = time.Second })
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status := protocol.NodeConnection{State: "reconnecting", Error: err.Error(), At: protocol.Now(), RetryAt: time.Now().Add(delay).UTC().Format(time.RFC3339Nano)}
		if errors.Is(err, errPairingRequired) {
			status.State = "pairing-required"
		}
		_ = n.Store.Put("status", "connection", status)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, 20*time.Second)
	}
}

func (n *Node) connect(ctx context.Context, instanceID string, connected func()) error {
	b, err := os.ReadFile(n.Config.NodeTokenFile)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(b))
	u, err := url.Parse(n.Config.HubURL)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else if u.Scheme == "http" {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/node/ws"
	dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
	conn, response, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPClient: hubHTTP, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	dialCancel()
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return errPairingRequired
		}
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 * 1024 * 1024)
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	send := func(f protocol.Frame) error {
		mu.Lock()
		defer mu.Unlock()
		c, done := context.WithTimeout(connectionCtx, 10*time.Second)
		defer done()
		return conn.Write(c, websocket.MessageText, protocol.JSON(f))
	}
	device := n.Device
	if catalog, ok := n.Executor.(interface{ Projects() []protocol.Project }); ok {
		device.Projects = catalog.Projects()
	}
	if err = send(protocol.Frame{Version: protocol.Version, Type: "hello", ID: instanceID, Payload: protocol.JSON(device)}); err != nil {
		return err
	}
	helloCtx, helloCancel := context.WithTimeout(connectionCtx, 10*time.Second)
	_, hello, err := conn.Read(helloCtx)
	helloCancel()
	if err != nil {
		return err
	}
	var accepted protocol.Frame
	if json.Unmarshal(hello, &accepted) == nil && accepted.Type == "error" {
		var detail struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(accepted.Payload, &detail) == nil && detail.Error != "" {
			return errors.New(detail.Error)
		}
	}
	if json.Unmarshal(hello, &accepted) != nil || accepted.Type != "hello.accepted" || accepted.Version != protocol.Version {
		return errors.New("Hub rejected protocol handshake")
	}
	connected()
	if controller, ok := n.Executor.(interface{ ComputerDisconnected() }); ok {
		defer controller.ComputerDisconnected()
	}
	_ = n.Store.Put("status", "connection", protocol.NodeConnection{State: "connected", At: protocol.Now()})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case <-connectionCtx.Done():
				return
			case <-ticker.C:
				ticks++
				events, err := n.Store.Unacked()
				if err != nil {
					cancel()
					return
				}
				for _, e := range events {
					if err = send(protocol.Frame{Version: protocol.Version, Type: "event", ID: e.ID, Payload: protocol.JSON(e)}); err != nil {
						cancel()
						return
					}
				}
				if ticks%15 == 0 {
					if err = send(protocol.Frame{Version: protocol.Version, Type: "heartbeat"}); err != nil {
						cancel()
						return
					}
				}
			}
		}
	}()
	for {
		readCtx, readCancel := context.WithTimeout(connectionCtx, 45*time.Second)
		_, b, err = conn.Read(readCtx)
		readCancel()
		if err != nil {
			return err
		}
		var f protocol.Frame
		if json.Unmarshal(b, &f) != nil || f.Version != protocol.Version {
			return errors.New("invalid Hub frame")
		}
		switch f.Type {
		case "event.ack":
			if err = n.Store.Ack(f.ID); err != nil {
				return err
			}
		case "command":
			if n.Executor == nil {
				return errors.New("this node does not provide task execution")
			}
			var c protocol.Command
			if json.Unmarshal(f.Payload, &c) != nil {
				return errors.New("invalid command")
			}
			go func() {
				taskCtx, done := context.WithTimeout(ctx, 45*time.Second)
				defer done()
				_ = n.Executor.Handle(taskCtx, c)
			}()
		case "heartbeat.ack":
		case "error":
			return errors.New("Hub protocol error")
		}
	}
}
