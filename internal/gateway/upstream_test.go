package gateway

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mewdex/internal/config"
)

func TestUpstreamProxyPreservesEndToEndVerifiedTLS(t *testing.T) {
	model := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.Header.Get("Authorization") != "Bearer model-credential" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("model request or its credential changed in transit")
		}
		_, _ = io.WriteString(w, "model reached through configured egress")
	}))
	defer model.Close()
	modelURL, _ := url.Parse(model.URL)
	var proxyRequests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		if r.Method != http.MethodConnect || r.Host != "example.com:443" || r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("upstream proxy received an unexpected target or credential")
		}
		upstream, err := net.Dial("tcp", modelURL.Host)
		if err != nil {
			t.Error(err)
			http.Error(w, "dial failed", 502)
			return
		}
		defer upstream.Close()
		downstream, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer downstream.Close()
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffer.Flush()
		copyTunnel(upstream, downstream, buffer)
	}))
	defer proxy.Close()
	authorized, _, _ := tunnelGateway(t)
	g, err := New(config.Gateway{AllowedHosts: []string{"example.com"}, UpstreamProxyURL: proxy.URL}, authorized.authorize)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		t.Error("Hub resolved a model destination while an upstream proxy was selected")
		return nil, errors.New("unexpected Hub DNS lookup")
	}
	f := testForwarder(t, hubTunnel(g))
	localProxy, _ := url.Parse(f.URL())
	localProxy.User = url.UserPassword("server", f.localToken)
	roots := x509.NewCertPool()
	roots.AddCert(model.Certificate())
	transport := model.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(localProxy)
	transport.TLSClientConfig.RootCAs = roots
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	request, _ := http.NewRequest("GET", "https://example.com/probe", nil)
	request.Header.Set("Authorization", "Bearer model-credential")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "model reached through configured egress" || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 || proxyRequests.Load() != 1 {
		t.Fatalf("unexpected proxied TLS result: %q %v", body, err)
	}
}

func TestUpstreamProxyPreservesBufferedData(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\nearly bytes")
		_ = buffer.Flush()
		_, _ = io.Copy(connection, buffer)
	}))
	defer proxy.Close()
	g, _, _ := tunnelGateway(t)
	g.upstreamProxy, _ = url.Parse(proxy.URL)
	g.dial = (&net.Dialer{}).DialContext
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, err := g.open(ctx, "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err = connection.Write([]byte{0, 255, 1, 2}); err != nil {
		t.Fatal(err)
	}
	want := append([]byte("early bytes"), 0, 255, 1, 2)
	got := make([]byte, len(want))
	if _, err = io.ReadFull(connection, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("proxy buffered data changed: %v %v", got, err)
	}
}

func TestUpstreamProxyFailuresDoNotUseDirectEgress(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rejected", http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	g, _, _ := tunnelGateway(t)
	g.upstreamProxy, _ = url.Parse(proxy.URL)
	var dials atomic.Int64
	g.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		if address != g.upstreamProxy.Host {
			t.Error("gateway attempted a direct model connection")
			return nil, errors.New("direct connection forbidden")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	for _, target := range []string{"example.com:80", "other.example.com:443", "127.0.0.1:443"} {
		if _, err := g.open(context.Background(), target); !errors.Is(err, errTargetDenied) {
			t.Fatalf("unapproved upstream target accepted: %s", target)
		}
	}
	if dials.Load() != 0 {
		t.Fatal("unapproved target reached the proxy")
	}
	if _, err := g.open(context.Background(), "example.com:443"); err == nil || !strings.Contains(err.Error(), "407") {
		t.Fatalf("proxy rejection was not returned: %v", err)
	}
	proxy.Close()
	if _, err := g.open(context.Background(), "example.com:443"); err == nil {
		t.Fatal("unavailable upstream proxy was accepted")
	}
	if dials.Load() != 2 {
		t.Fatal("proxy failure retried another route")
	}
}

func TestUpstreamProxyConfigurationAndTLSAreChecked(t *testing.T) {
	authorized, _, _ := tunnelGateway(t)
	for _, value := range []string{"http://public.example", "http://user:secret@127.0.0.1", "http://127.0.0.1?secret", "https://proxy.example/path", "socks5://127.0.0.1"} {
		if _, err := New(config.Gateway{AllowedHosts: []string{"example.com"}, UpstreamProxyURL: value}, authorized.authorize); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("unsafe proxy configuration was accepted or exposed: %v", err)
		}
	}
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("untrusted proxy TLS reached HTTP")
	}))
	defer proxy.Close()
	g, err := New(config.Gateway{AllowedHosts: []string{"example.com"}, UpstreamProxyURL: proxy.URL}, authorized.authorize)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if _, err = g.open(context.Background(), "example.com:443"); err == nil || !strings.Contains(err.Error(), "TLS handshake") {
		t.Fatalf("untrusted proxy TLS was accepted: %v", err)
	}
}
