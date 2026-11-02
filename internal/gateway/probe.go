package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"mewdex/internal/config"
)

// Probe verifies an explicit proxy route with TLS, without sending model credentials or inference.
func Probe(ctx context.Context, c config.Engine) (map[string]any, error) {
	if c.ProxyURL == "" {
		return nil, errors.New("engine.proxyUrl is not configured")
	}
	proxy, err := c.AuthenticatedProxyURL()
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(proxy)
	transport := &http.Transport{Proxy: http.ProxyURL(u), TLSHandshakeTimeout: 15 * time.Second}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 25 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://chatgpt.com/backend-api/codex/models", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("official service was not reached through the configured gateway")
	}
	defer response.Body.Close()
	return map[string]any{"gatewayReachable": true, "upstreamHost": "chatgpt.com", "upstreamStatus": response.StatusCode, "tlsVerified": response.TLS != nil && len(response.TLS.VerifiedChains) > 0, "modelCredentialsSent": false, "inferenceRun": false}, nil
}
