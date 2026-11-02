package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/config"
)

// Forwarder exposes an authenticated, ephemeral loopback CONNECT proxy for the
// local engine. Its upstream is the paired Hub, never a caller-supplied URL.
type Forwarder struct {
	server      *http.Server
	listener    net.Listener
	endpoint    string
	device      string
	token       string
	localToken  string
	client      *http.Client
	ctx         context.Context
	closeOnce   sync.Once
	mu          sync.Mutex
	connections map[net.Conn]bool
	closed      bool
	closedCh    chan struct{}
}

func StartForwarder(ctx context.Context, c config.Config) (*Forwarder, error) {
	if c.Hub || !c.Executor || c.HubURL == "" || c.NodeTokenFile == "" || c.DeviceID == "" {
		return nil, errors.New("Hub forwarding requires a paired remote executor")
	}
	if err := config.ValidateURL(c.HubURL, c.AllowPrivateHTTP); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(c.NodeTokenFile)
	if err != nil {
		return nil, errors.New("cannot read executor credential")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return nil, errors.New("executor credential is empty")
	}
	u, _ := url.Parse(c.HubURL)
	if u.Scheme == "https" || u.Scheme == "wss" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/gateway/ws"
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f := &Forwarder{listener: listener, endpoint: u.String(), device: c.DeviceID, token: token, localToken: rand.Text(), ctx: ctx,
		client:      &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone(), CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Hub redirects are not supported") }},
		connections: map[net.Conn]bool{}, closedCh: make(chan struct{})}
	f.server = &http.Server{Handler: f, ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() { _ = f.server.Serve(listener) }()
	go func() {
		select {
		case <-ctx.Done():
			f.Close()
		case <-f.closedCh:
		}
	}()
	return f, nil
}

// URL contains no credential. The engine adds its own device credential only
// to this local proxy; the Hub handshake uses the same identity as node traffic.
func (f *Forwarder) URL() string { return "http://" + f.listener.Addr().String() }

func (f *Forwarder) EngineConfig(c config.Engine) config.Engine {
	c.ProxyURL, c.ProxyUsername = f.URL(), f.device
	c.ProxyCredential, c.ProxyTokenFile = f.localToken, ""
	return c
}

func (f *Forwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Header.Get("Proxy-Authorization"), "Basic "))
	device, token, ok := strings.Cut(string(raw), ":")
	if err != nil || !ok || subtle.ConstantTimeCompare([]byte(device), []byte(f.device)) != 1 || subtle.ConstantTimeCompare([]byte(token), []byte(f.localToken)) != 1 {
		w.Header().Set("Proxy-Authenticate", `Basic realm="MewDex local engine"`)
		http.Error(w, "proxy authentication required", 407)
		return
	}
	if r.Method != http.MethodConnect {
		http.Error(w, "HTTPS CONNECT required", 405)
		return
	}
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || host == "" || port != "443" {
		http.Error(w, "HTTPS target required", 403)
		return
	}
	u, _ := url.Parse(f.endpoint)
	query := u.Query()
	query.Set("target", r.Host)
	u.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	socket, response, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: f.client, HTTPHeader: http.Header{"Authorization": {"Bearer " + f.token}}})
	cancel()
	if err != nil {
		status := http.StatusBadGateway
		if response != nil && (response.StatusCode == 401 || response.StatusCode == 403) {
			status = http.StatusForbidden
		}
		http.Error(w, "Hub gateway connection failed", status)
		return
	}
	upstream := fastSocketConn(f.ctx, socket)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "CONNECT unavailable", 500)
		return
	}
	downstream, buffer, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		upstream.Close()
		downstream.Close()
		return
	}
	f.connections[upstream], f.connections[downstream] = true, true
	f.mu.Unlock()
	defer func() {
		upstream.Close()
		downstream.Close()
		f.mu.Lock()
		delete(f.connections, upstream)
		delete(f.connections, downstream)
		f.mu.Unlock()
	}()
	if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffer.Flush(); err != nil {
		return
	}
	copyTunnel(upstream, downstream, buffer)
}

func (f *Forwarder) Close() {
	f.closeOnce.Do(func() {
		_ = f.server.Close()
		f.mu.Lock()
		f.closed = true
		for conn := range f.connections {
			conn.Close()
		}
		f.mu.Unlock()
		f.client.CloseIdleConnections()
		close(f.closedCh)
	})
}
