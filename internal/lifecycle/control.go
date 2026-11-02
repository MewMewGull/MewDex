package lifecycle

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mewdex/internal/protocol"
)

type Record struct {
	DeviceID   string `json:"deviceId"`
	InstanceID string `json:"instanceId"`
	Address    string `json:"address"`
	Token      string `json:"token"`
	PID        int    `json:"pid"`
	StartedAt  string `json:"startedAt"`
}
type Control struct {
	server *http.Server
	record Record
	path   string
	mu     sync.RWMutex
	state  string
	hubURL string
}

func Start(dir, device string, stop context.CancelFunc) (*Control, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	record := Record{DeviceID: device, InstanceID: protocol.ID(), Address: "http://" + listener.Addr().String(), Token: protocol.ID() + protocol.ID(), PID: os.Getpid(), StartedAt: protocol.Now()}
	path := filepath.Join(dir, "control.json")
	if err = os.WriteFile(path, protocol.JSON(record), 0600); err != nil {
		listener.Close()
		return nil, err
	}
	control := &Control{record: record, path: path, state: "starting"}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+record.Token)) != 1 || r.Header.Get("X-MewDex-Instance") != record.InstanceID {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		switch {
		case r.Method == "GET" && r.URL.Path == "/status":
			control.mu.RLock()
			state, hubURL := control.state, control.hubURL
			control.mu.RUnlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"deviceId": device, "instanceId": record.InstanceID, "pid": record.PID, "startedAt": record.StartedAt, "state": state, "ready": state == "running", "hubUrl": hubURL})
		case r.Method == "POST" && r.URL.Path == "/stop":
			control.mu.Lock()
			control.state = "stopping"
			control.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "stopping"})
			stop()
		default:
			http.Error(w, "not found", 404)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	control.server = server
	go func() { _ = server.Serve(listener) }()
	return control, nil
}

func (c *Control) Ready(hubURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == "starting" {
		c.state, c.hubURL = "running", hubURL
	}
}

func (c *Control) InstanceID() string { return c.record.InstanceID }

func (c *Control) Close() error {
	err := c.server.Close()
	b, readErr := os.ReadFile(c.path)
	if readErr == nil {
		var current Record
		if json.Unmarshal(b, &current) == nil && current.InstanceID == c.record.InstanceID {
			_ = os.Remove(c.path)
		}
	}
	return err
}

func Read(dir string) (Record, error) {
	var record Record
	b, err := os.ReadFile(filepath.Join(dir, "control.json"))
	if err != nil {
		return record, errors.New("no local service control record")
	}
	err = json.Unmarshal(b, &record)
	return record, err
}

func Call(ctx context.Context, record Record, stop bool) (map[string]any, error) {
	method, path := "GET", "/status"
	if stop {
		method, path = "POST", "/stop"
	}
	url := record.Address + path
	parsed, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(parsed.URL.Host)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		return nil, errors.New("control address must be loopback")
	}
	parsed.Header.Set("Authorization", "Bearer "+record.Token)
	parsed.Header.Set("X-MewDex-Instance", record.InstanceID)
	client := http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(parsed)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("control operation rejected: %d", response.StatusCode)
	}
	var result map[string]any
	err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result)
	if err == nil && !stop && (result["deviceId"] != record.DeviceID || result["instanceId"] != record.InstanceID) {
		return nil, errors.New("control response belongs to a different service instance")
	}
	return result, err
}
