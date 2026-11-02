// Package gateway implements authenticated CONNECT forwarding without terminating model TLS.
package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/config"
)

type Gateway struct {
	hosts       map[string]bool
	authorize   Authorizer
	mu          sync.Mutex
	connections map[net.Conn]Grant
	opened      atomic.Int64
	devices     map[string]int64
	closed      bool
	lookup      func(context.Context, string) ([]net.IPAddr, error)
	dial        func(context.Context, string, string) (net.Conn, error)
	checkEvery  time.Duration
}

type Grant struct {
	IdentityID string `json:"identityId"`
	DeviceID   string `json:"deviceId"`
}
type Authorizer func(context.Context, string, string) (Grant, error)

func (g *Gateway) Snapshot() any {
	g.mu.Lock()
	active := len(g.connections) / 2
	devices := map[string]int64{}
	for id, count := range g.devices {
		devices[id] = count
	}
	g.mu.Unlock()
	return map[string]any{"openedTunnels": g.opened.Load(), "activeTunnels": active, "devices": devices}
}

func New(c config.Gateway, authorize Authorizer) (*Gateway, error) {
	if authorize == nil || len(c.AllowedHosts) == 0 {
		return nil, errors.New("gateway requires an identity authorizer and explicit allowedHosts")
	}
	g := &Gateway{hosts: map[string]bool{}, authorize: authorize, connections: map[net.Conn]Grant{}, devices: map[string]int64{},
		lookup: net.DefaultResolver.LookupIPAddr, dial: (&net.Dialer{Timeout: 15 * time.Second}).DialContext, checkEvery: 5 * time.Second}
	for _, host := range c.AllowedHosts {
		if host == "" || strings.ContainsAny(host, "/:* ?@#\\[]\r\n\t") {
			return nil, errors.New("allowedHosts must contain exact hostnames")
		}
		g.hosts[strings.ToLower(host)] = true
	}
	return g, nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, decodeErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Header.Get("Proxy-Authorization"), "Basic "))
	username, token, ok := strings.Cut(string(raw), ":")
	var grant Grant
	var authErr error
	if decodeErr == nil && ok && username != "" && token != "" {
		grant, authErr = g.authorized(r.Context(), username, token)
	} else {
		authErr = errors.New("invalid proxy credentials")
	}
	if authErr != nil {
		w.Header().Set("Proxy-Authenticate", `Basic realm="MewDex"`)
		http.Error(w, "proxy authentication required", 407)
		return
	}
	if r.Method != "CONNECT" {
		http.Error(w, "HTTPS CONNECT required", 405)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	upstream, err := g.open(ctx, r.Host)
	if err != nil {
		targetProblem(w, err)
		return
	}
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
	if !g.track(grant, upstream, downstream) {
		upstream.Close()
		downstream.Close()
		return
	}
	defer g.untrack(upstream, downstream)
	stopWatching, err := g.watch(r.Context(), grant, username, token, upstream, downstream)
	if err != nil {
		return
	}
	defer stopWatching()
	if _, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffer.Flush(); err != nil {
		return
	}
	copyTunnel(upstream, downstream, buffer)
}

var errTargetDenied = errors.New("gateway target is not allowed")

func targetProblem(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, errTargetDenied) {
		status = http.StatusForbidden
	}
	http.Error(w, err.Error(), status)
}

func (g *Gateway) open(ctx context.Context, authority string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || port != "443" || !g.hosts[strings.ToLower(host)] {
		return nil, errTargetDenied
	}
	addresses, err := g.lookup(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("gateway upstream resolution failed")
	}
	var chosen net.IP
	for _, address := range addresses {
		ip := address.IP
		if !publicAddress(ip) {
			continue
		}
		chosen = ip
		break
	}
	if chosen == nil {
		return nil, errTargetDenied
	}
	upstream, err := g.dial(ctx, "tcp", net.JoinHostPort(chosen.String(), port))
	if err != nil {
		return nil, errors.New("gateway upstream connection failed")
	}
	return upstream, nil
}

// ServeTunnel is called by a Hub only after authenticating an executor identity
// and checking its gateway grant. Official model TLS stays inside this byte stream.
func (g *Gateway) ServeTunnel(w http.ResponseWriter, r *http.Request, grant Grant, token string) {
	if grant.IdentityID == "" || grant.DeviceID == "" {
		http.Error(w, "gateway grant required", 403)
		return
	}
	current, err := g.authorized(r.Context(), grant.DeviceID, token)
	if err != nil || current != grant {
		http.Error(w, "gateway grant is no longer valid", 403)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	upstream, err := g.open(ctx, r.URL.Query().Get("target"))
	cancel()
	if err != nil {
		targetProblem(w, err)
		return
	}
	socket, err := websocket.Accept(w, r, nil)
	if err != nil {
		upstream.Close()
		return
	}
	downstream := fastSocketConn(r.Context(), socket)
	if !g.track(grant, upstream, downstream) {
		upstream.Close()
		downstream.Close()
		return
	}
	defer g.untrack(upstream, downstream)
	stopWatching, err := g.watch(r.Context(), grant, grant.DeviceID, token, upstream, downstream)
	if err != nil {
		return
	}
	defer stopWatching()
	copyTunnel(upstream, downstream, downstream)
}

func (g *Gateway) authorized(ctx context.Context, device, token string) (Grant, error) {
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	grant, err := g.authorize(check, device, token)
	if err != nil || grant.IdentityID == "" || grant.DeviceID != device {
		return Grant{}, errors.New("device is not authorized to use this gateway")
	}
	return grant, nil
}

// Recheck after registration closes the gap between authorization and a
// simultaneous revocation. Periodic checks also cover a standalone gateway.
func (g *Gateway) watch(ctx context.Context, grant Grant, device, token string, upstream, downstream net.Conn) (context.CancelFunc, error) {
	check := func(ctx context.Context) bool {
		current, err := g.authorized(ctx, device, token)
		return err == nil && current == grant
	}
	if !check(ctx) {
		return nil, errors.New("gateway grant was revoked")
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(g.checkEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !check(ctx) {
					upstream.Close()
					downstream.Close()
					return
				}
			}
		}
	}()
	return cancel, nil
}

func (g *Gateway) track(grant Grant, upstream, downstream net.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.connections[upstream] = grant
	g.connections[downstream] = grant
	g.opened.Add(1)
	g.devices[grant.DeviceID]++
	return true
}

func (g *Gateway) untrack(upstream, downstream net.Conn) {
	upstream.Close()
	downstream.Close()
	g.mu.Lock()
	delete(g.connections, upstream)
	delete(g.connections, downstream)
	g.mu.Unlock()
}

func copyTunnel(upstream, downstream net.Conn, input io.Reader) {
	done := make(chan struct{})
	go func() { _, _ = io.Copy(upstream, input); _ = upstream.Close(); close(done) }()
	_, _ = io.Copy(downstream, upstream)
	_ = downstream.Close()
	<-done
}

type socketConn struct {
	net.Conn
	socket *websocket.Conn
}

func (c *socketConn) Close() error { return c.socket.CloseNow() }
func fastSocketConn(ctx context.Context, c *websocket.Conn) net.Conn {
	return &socketConn{Conn: websocket.NetConn(ctx, c, websocket.MessageBinary), socket: c}
}

func (g *Gateway) Revoke(identityID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for conn, grant := range g.connections {
		if grant.IdentityID == identityID {
			_ = conn.Close()
		}
	}
}

func (g *Gateway) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	for conn := range g.connections {
		_ = conn.Close()
	}
}
