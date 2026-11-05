package config

import (
	"net"
	"net/url"
)

// HubModelEngine adopts an older Hub's local proxy choice when its shared
// Gateway listener is retired. New profiles configure the model engine directly.
func (c Config) HubModelEngine() Engine {
	engine := c.Engine
	engine.UseHubGateway = false
	if c.Gateway == nil || engine.ProxyTokenFile == "" {
		return engine
	}
	proxy, err := url.Parse(engine.ProxyURL)
	if err != nil {
		return engine
	}
	ip := net.ParseIP(proxy.Hostname())
	if proxy.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return engine
	}
	listen := c.Gateway.Listen
	if listen == "" {
		listen = "127.0.0.1:47831"
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil || proxy.Port() != port {
		return engine
	}
	engine.ProxyURL = c.Gateway.UpstreamProxyURL
	engine.ProxyUsername, engine.ProxyTokenFile, engine.ProxyCredential = "", "", ""
	return engine
}
