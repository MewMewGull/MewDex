package gateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mewdex/internal/config"
)

func tunnelGateway(t *testing.T) (*Gateway, *atomic.Bool, *atomic.Int64) {
	t.Helper()
	allowed, dials := &atomic.Bool{}, &atomic.Int64{}
	allowed.Store(true)
	g, err := New(config.Gateway{AllowedHosts: []string{"example.com"}}, func(_ context.Context, device, token string) (Grant, error) {
		if !allowed.Load() || device != "server" || token != "node-token" {
			return Grant{}, errors.New("denied")
		}
		return Grant{IdentityID: "node", DeviceID: device}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	g.lookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "example.com" {
			t.Errorf("unexpected DNS name: %s", host)
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("1.1.1.1")}}, nil
	}
	g.dial = func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "1.1.1.1:443" {
			t.Errorf("DNS result was not pinned: %s %s", network, address)
		}
		dials.Add(1)
		local, remote := net.Pipe()
		go func() { defer remote.Close(); _, _ = io.Copy(remote, remote) }()
		return local, nil
	}
	t.Cleanup(g.Close)
	return g, allowed, dials
}

func testForwarder(t *testing.T, handler http.Handler) *Forwarder {
	t.Helper()
	hub := httptest.NewServer(handler)
	t.Cleanup(hub.Close)
	path := filepath.Join(t.TempDir(), "node-token")
	if err := os.WriteFile(path, []byte("node-token"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := StartForwarder(context.Background(), config.Config{HubURL: hub.URL, DeviceID: "server", Executor: true, NodeTokenFile: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

func hubTunnel(g *Gateway) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/gateway/ws" || r.Header.Get("Authorization") != "Bearer node-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		g.ServeTunnel(w, r, Grant{IdentityID: "node", DeviceID: "server"}, "node-token")
	})
}

func connectProxy(t *testing.T, proxy, credentials, target string, pipelined []byte) (net.Conn, *bufio.Reader, int) {
	t.Helper()
	u, err := url.Parse(proxy)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = conn.SetDeadline(time.Now().Add(4 * time.Second)); err != nil {
		t.Fatal(err)
	}
	headers := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", target, target, base64.StdEncoding.EncodeToString([]byte(credentials)))
	writeDone := make(chan error, 1)
	go func() { _, err := conn.Write(append([]byte(headers), pipelined...)); writeDone <- err }()
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-writeDone; err != nil {
		t.Fatal(err)
	}
	return conn, reader, response.StatusCode
}

func TestConnectAndHubTunnelPreservePipelinedBinaryAndRevoke(t *testing.T) {
	for _, websocket := range []bool{false, true} {
		t.Run(fmt.Sprintf("websocket=%v", websocket), func(t *testing.T) {
			g, _, _ := tunnelGateway(t)
			var proxy string
			credential := "server:node-token"
			if websocket {
				f := testForwarder(t, hubTunnel(g))
				proxy, credential = f.URL(), "server:"+f.localToken
			} else {
				server := httptest.NewServer(g)
				t.Cleanup(server.Close)
				proxy = server.URL
			}
			payload := bytes.Repeat([]byte{0, 255, 13, 10, 7, 22, 3, 1}, 16384)
			conn, reader, status := connectProxy(t, proxy, credential, "example.com:443", payload)
			if status != 200 {
				t.Fatalf("CONNECT: %d", status)
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(reader, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, got) {
				t.Fatal("tunnel changed opaque bytes")
			}
			g.Revoke("node")
			if _, err := conn.Read(make([]byte, 1)); err == nil {
				t.Fatal("revoked tunnel remained open")
			} else if e, ok := err.(net.Error); ok && e.Timeout() {
				t.Fatal("revocation waited for deadline")
			}
		})
	}
}

func TestForwarderPreservesEndToEndVerifiedTLS(t *testing.T) {
	g, _, _ := tunnelGateway(t)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.Host != "example.com" || r.URL.Path != "/probe" {
			t.Error("upstream did not receive original TLS request")
		}
		if r.Header.Get("Authorization") != "Bearer model-credential" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credential crossed into model request")
		}
		_, _ = w.Write([]byte("verified opaque TLS"))
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	g.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", upstreamURL.Host)
	}
	f := testForwarder(t, hubTunnel(g))
	proxy, _ := url.Parse(f.URL())
	proxy.User = url.UserPassword("server", f.localToken)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	transport := upstream.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxy)
	transport.TLSClientConfig.RootCAs = roots
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	req, _ := http.NewRequest("GET", "https://example.com/probe", nil)
	req.Header.Set("Authorization", "Bearer model-credential")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "verified opaque TLS" || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
		t.Fatalf("TLS result: %q %v", body, err)
	}
}

func TestTargetsAndLocalCredentialsAreCheckedBeforeDial(t *testing.T) {
	g, _, dials := tunnelGateway(t)
	f := testForwarder(t, hubTunnel(g))
	for _, tc := range []struct {
		credential, target string
		status             int
	}{
		{"server:wrong", "example.com:443", 407},
		{"another:node-token", "example.com:443", 407},
		{"server:" + f.localToken, "example.com:80", 403},
		{"server:" + f.localToken, "other.example.com:443", 403},
		{"server:" + f.localToken, "127.0.0.1:443", 403},
	} {
		conn, _, status := connectProxy(t, f.URL(), tc.credential, tc.target, nil)
		conn.Close()
		if status != tc.status {
			t.Errorf("%s: %d want %d", tc.target, status, tc.status)
		}
	}
	if dials.Load() != 0 {
		t.Fatal("denied request reached upstream")
	}
	g.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.1.2.3")}, {IP: net.ParseIP("::1")}}, nil
	}
	if _, err := g.open(context.Background(), "example.com:443"); !errors.Is(err, errTargetDenied) || dials.Load() != 0 {
		t.Fatal("private DNS result reached upstream")
	}
}

func TestGatewayRechecksGrantAfterDialAndDuringConnection(t *testing.T) {
	t.Run("revoked while dialing", func(t *testing.T) {
		g, allowed, _ := tunnelGateway(t)
		dial := g.dial
		g.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			allowed.Store(false)
			return dial(ctx, network, address)
		}
		f := testForwarder(t, hubTunnel(g))
		conn, reader, status := connectProxy(t, f.URL(), "server:"+f.localToken, "example.com:443", nil)
		defer conn.Close()
		if status == 200 {
			if _, err := reader.ReadByte(); err == nil {
				t.Fatal("revocation race left a usable tunnel")
			}
		}
	})
	t.Run("standalone periodic recheck", func(t *testing.T) {
		g, allowed, _ := tunnelGateway(t)
		g.checkEvery = 5 * time.Millisecond
		f := testForwarder(t, hubTunnel(g))
		conn, reader, status := connectProxy(t, f.URL(), "server:"+f.localToken, "example.com:443", []byte("hello"))
		if status != 200 {
			t.Fatal(status)
		}
		if _, err := io.ReadFull(reader, make([]byte, 5)); err != nil {
			t.Fatal(err)
		}
		allowed.Store(false)
		if _, err := conn.Read(make([]byte, 1)); err == nil {
			t.Fatal("removed grant remained usable")
		} else if e, ok := err.(net.Error); ok && e.Timeout() {
			t.Fatal("grant change did not close tunnel")
		}
	})
}

func TestForwarderClosesHijackedConnectionsAndDoesNotFollowRedirects(t *testing.T) {
	t.Run("service shutdown", func(t *testing.T) {
		g, _, _ := tunnelGateway(t)
		f := testForwarder(t, hubTunnel(g))
		conn, reader, status := connectProxy(t, f.URL(), "server:"+f.localToken, "example.com:443", []byte("hello"))
		if status != 200 {
			t.Fatal(status)
		}
		if _, err := io.ReadFull(reader, make([]byte, 5)); err != nil {
			t.Fatal(err)
		}
		f.Close()
		if _, err := conn.Read(make([]byte, 1)); err == nil {
			t.Fatal("shutdown left hijacked connection open")
		}
	})
	t.Run("redirect", func(t *testing.T) {
		var leaked atomic.Bool
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
		defer destination.Close()
		f := testForwarder(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
		conn, _, status := connectProxy(t, f.URL(), "server:"+f.localToken, "example.com:443", nil)
		conn.Close()
		if status != 502 || leaked.Load() {
			t.Fatalf("redirect followed: %d %v", status, leaked.Load())
		}
	})
}

func TestForwarderRejectsPublicPlainHTTP(t *testing.T) {
	_, err := StartForwarder(context.Background(), config.Config{DeviceID: "server", Executor: true, HubURL: "http://public.example", NodeTokenFile: "not-read"})
	if err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatal("public Hub did not require TLS")
	}
}

func TestChildProxyCredentialCannotBeUsedAsNodeIdentity(t *testing.T) {
	g, _, _ := tunnelGateway(t)
	f := testForwarder(t, hubTunnel(g))
	c := f.EngineConfig(config.Engine{UseHubGateway: true, ProxyTokenFile: "old-file"})
	if c.ProxyCredential == "" || c.ProxyCredential == "node-token" || c.ProxyTokenFile != "" {
		t.Fatal("child inherited the node credential")
	}
	if _, err := g.authorized(context.Background(), "server", c.ProxyCredential); err == nil {
		t.Fatal("local child credential became a node identity")
	}
	serialized, err := json.Marshal(c)
	if err != nil || bytes.Contains(serialized, []byte(c.ProxyCredential)) || bytes.Contains(serialized, []byte("node-token")) {
		t.Fatal("transient credential was serialized")
	}
	authenticated, err := c.AuthenticatedProxyURL()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authenticated)
	if err != nil {
		t.Fatal(err)
	}
	secret, ok := u.User.Password()
	if !ok || secret != c.ProxyCredential || u.Hostname() != "127.0.0.1" {
		t.Fatal("engine did not receive its own loopback credential")
	}
}

func TestSpecialDNSDestinationsCannotBecomeGatewayUpstreams(t *testing.T) {
	g, _, dials := tunnelGateway(t)
	for _, address := range []string{"100.100.100.200", "0.0.0.1", "198.19.0.1", "203.0.113.7", "240.1.2.3", "::ffff:10.0.0.1", "2001:db8::1", "2002:0a00:0001::1", "3fff::1", "64:ff9b::a00:1", "64:ff9b::6464:64c8"} {
		g.lookup = func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
		}
		if _, err := g.open(context.Background(), "example.com:443"); !errors.Is(err, errTargetDenied) {
			t.Errorf("special destination was accepted: %s", address)
		}
	}
	if dials.Load() != 0 {
		t.Fatal("special address reached a dialer")
	}
	for _, address := range []string{"1.1.1.1", "2606:4700:4700::1111", "::ffff:1.1.1.1", "64:ff9b::101:101"} {
		if !publicAddress(net.ParseIP(address)) {
			t.Errorf("public destination was rejected: %s", address)
		}
	}
}
