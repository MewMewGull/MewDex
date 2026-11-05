package config

import "testing"

func TestHubModelConnectionAdoptsLegacyLocalGatewayProxy(t *testing.T) {
	c := Config{Engine: Engine{ProxyURL: "http://127.0.0.1:47831", ProxyTokenFile: "old-device-token", ProxyUsername: "node"}, Gateway: &Gateway{Listen: "127.0.0.1:47831", UpstreamProxyURL: "http://127.0.0.1:7890"}}
	engine := c.HubModelEngine()
	if engine.ProxyURL != c.Gateway.UpstreamProxyURL || engine.ProxyTokenFile != "" || engine.ProxyUsername != "" {
		t.Fatal(engine)
	}
	c.Engine.ProxyURL = "https://proxy.example.com"
	if engine = c.HubModelEngine(); engine.ProxyTokenFile != c.Engine.ProxyTokenFile || engine.ProxyURL != c.Engine.ProxyURL {
		t.Fatal("an independent authenticated proxy changed", engine)
	}
}
