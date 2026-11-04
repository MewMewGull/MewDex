package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// The configured proxy resolves the allowed model hostname at its own egress.
// Node authorization stays on the Hub and model TLS passes through unchanged.
func (g *Gateway) openProxy(ctx context.Context, authority string) (net.Conn, error) {
	proxy := g.upstreamProxy
	port := proxy.Port()
	if port == "" {
		port = "80"
		if proxy.Scheme == "https" {
			port = "443"
		}
	}
	raw, err := g.dial(ctx, "tcp", net.JoinHostPort(proxy.Hostname(), port))
	if err != nil {
		return nil, errors.New("gateway upstream proxy connection failed")
	}
	succeeded := false
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer func() {
		stop()
		if !succeeded {
			_ = raw.Close()
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	if err = raw.SetDeadline(deadline); err != nil {
		return nil, errors.New("gateway upstream proxy deadline failed")
	}
	var connection net.Conn = raw
	if proxy.Scheme == "https" {
		secured := tls.Client(raw, &tls.Config{ServerName: proxy.Hostname(), MinVersion: tls.VersionTLS12})
		if err = secured.HandshakeContext(ctx); err != nil {
			return nil, errors.New("gateway upstream proxy TLS handshake failed")
		}
		connection = secured
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: authority}, Host: authority}
	if err = request.Write(connection); err != nil {
		return nil, errors.New("gateway upstream proxy CONNECT failed")
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, errors.New("gateway upstream proxy response failed")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway upstream proxy rejected CONNECT with HTTP %d", response.StatusCode)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err = connection.SetDeadline(time.Time{}); err != nil {
		return nil, errors.New("gateway upstream proxy deadline reset failed")
	}
	succeeded = true
	return &proxyConnection{Conn: connection, reader: reader}, nil
}

type proxyConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (c *proxyConnection) Read(p []byte) (int, error) { return c.reader.Read(p) }
