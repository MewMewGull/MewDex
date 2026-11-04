package hub

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/gateway"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"mewdex/internal/transport"
)

func startGatewayProvider(t *testing.T, h *Hub, s *store.Store, base string, provider *gateway.Gateway) context.CancelFunc {
	t.Helper()
	code, err := s.Invite("executor", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Pair(code, "Gateway egress", "egress-node")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	tokenFile := filepath.Join(root, "node-token")
	if err = os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(root, "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	node := &transport.Node{Config: config.Config{DeviceID: "egress-node", HubURL: base, NodeTokenFile: tokenFile}, Store: state, Gateway: provider,
		Device: protocol.Device{ID: "egress-node", Roles: []string{"gateway"}, Capabilities: []string{"model-gateway-relay"}, ProtocolVersion: protocol.Version}}
	done := make(chan error, 1)
	go func() { done <- node.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("gateway node did not stop")
		}
		_ = state.Close()
	})
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		h.mu.Lock()
		connected := h.peers["egress-node"] != nil
		h.mu.Unlock()
		if connected {
			return cancel
		}
	}
	t.Fatal("gateway provider did not register")
	return cancel
}

func TestNativeGatewayRelayPreservesModelTLSAndStopsWithProvider(t *testing.T) {
	h, s, server, _ := testHub(t)
	model := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.Header.Get("Authorization") != "Bearer model-credential" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("model request changed inside the native relay")
		}
		_, _ = io.WriteString(w, "native model egress")
	}))
	defer model.Close()
	modelURL, _ := url.Parse(model.URL)
	var proxyRequests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		if r.Method != http.MethodConnect || r.Host != "example.com:443" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("native gateway disclosed a credential or changed the target")
		}
		upstream, err := net.Dial("tcp", modelURL.Host)
		if err != nil {
			t.Error(err)
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
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, buffer)
			_ = upstream.Close()
			close(done)
		}()
		_, _ = io.Copy(downstream, upstream)
		_ = downstream.Close()
		<-done
	}))
	defer proxy.Close()
	provider, err := gateway.New(config.Gateway{AllowedHosts: []string{"example.com"}, UpstreamProxyURL: proxy.URL}, h.AuthorizeGateway)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	h.config.Gateway = &config.Gateway{AllowedHosts: []string{"example.com"}, ForwardDeviceID: "egress-node"}
	router, err := gateway.New(*h.config.Gateway, h.AuthorizeGateway)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	h.SetModelGateway(router)
	stopProvider := startGatewayProvider(t, h, s, server.URL, provider)
	code, _ := s.InviteWithAccess("executor", protocol.Access{Gateway: true}, time.Minute)
	token, _, err := s.Pair(code, "Task executor", "task-node")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "node-token")
	if err = os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	forwarder, err := gateway.StartForwarder(context.Background(), config.Config{HubURL: server.URL, DeviceID: "task-node", Executor: true, NodeTokenFile: path})
	if err != nil {
		t.Fatal(err)
	}
	defer forwarder.Close()
	engineProxy, err := forwarder.EngineConfig(config.Engine{}).AuthenticatedProxyURL()
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, _ := url.Parse(engineProxy)
	roots := x509.NewCertPool()
	roots.AddCert(model.Certificate())
	clientTransport := model.Client().Transport.(*http.Transport).Clone()
	clientTransport.Proxy = http.ProxyURL(proxyURL)
	clientTransport.TLSClientConfig.RootCAs = roots
	clientTransport.DisableKeepAlives = true
	defer clientTransport.CloseIdleConnections()
	client := &http.Client{Transport: clientTransport, Timeout: 3 * time.Second}
	requestModel := func() (*http.Response, error) {
		request, _ := http.NewRequest("GET", "https://example.com/model", nil)
		request.Header.Set("Authorization", "Bearer model-credential")
		return client.Do(request)
	}
	response, err := requestModel()
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "native model egress" || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 || proxyRequests.Load() != 1 {
		t.Fatalf("native relay result: %q %v", body, err)
	}
	stopProvider()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		h.mu.Lock()
		disconnected := h.peers["egress-node"] == nil
		h.mu.Unlock()
		if disconnected {
			break
		}
	}
	if response, err = requestModel(); err == nil {
		response.Body.Close()
		t.Fatal("model request succeeded after the designated provider disconnected")
	}
	if proxyRequests.Load() != 1 {
		t.Fatal("an offline provider retried the model request")
	}
}

func TestGatewayRelayRejectsAnotherIdentityAndUnboundRequests(t *testing.T) {
	h, s, server, admin := testHub(t)
	code, _ := s.Invite("executor", time.Minute)
	providerToken, providerIdentity, err := s.Pair(code, "Gateway egress", "egress-node")
	if err != nil {
		t.Fatal(err)
	}
	code, _ = s.Invite("executor", time.Minute)
	otherToken, _, _ := s.Pair(code, "other", "other")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &peer{ctx: ctx, identityID: providerIdentity.ID, device: protocol.Device{ID: "egress-node"}}
	relay := &gatewayRelay{provider: provider, ctx: ctx, result: make(chan gatewayRelayResult)}
	h.peers["egress-node"] = provider
	h.gatewayRelays["one-use-id"] = relay
	for _, token := range []string{admin, otherToken} {
		response := request(t, server.URL, token, "GET", "/v1/gateway/relay/ws?id=one-use-id", nil)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden || h.gatewayRelays["one-use-id"] != relay {
			t.Fatal("another identity claimed the provider's relay")
		}
	}
	response := request(t, server.URL, providerToken, "GET", "/v1/gateway/relay/ws?id=unknown", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("unbound relay request was accepted")
	}
	h.failGatewayRelay(&peer{}, "one-use-id")
	if h.gatewayRelays["one-use-id"] != relay {
		t.Fatal("another peer cancelled a pending relay")
	}
	go h.failGatewayRelay(provider, "one-use-id")
	select {
	case result := <-relay.result:
		if result.connection != nil || result.err == nil {
			t.Fatal("provider failure did not finish the pending request")
		}
	case <-time.After(time.Second):
		t.Fatal("provider failure was not delivered")
	}
	if _, err = h.OpenGateway(context.Background(), "example.com:443"); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unconfigured gateway forwarding was accepted")
	}
}
