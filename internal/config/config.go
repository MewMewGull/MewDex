package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"mewdex/internal/protocol"
	"mewdex/internal/workspace"
)

type Engine struct {
	Executable     string   `json:"executable"`
	Args           []string `json:"args,omitempty"`
	ProxyURL       string   `json:"proxyUrl,omitempty"`
	ProxyTokenFile string   `json:"proxyTokenFile,omitempty"`
	ProxyUsername  string   `json:"proxyUsername,omitempty"`
	Home           string   `json:"home,omitempty"`
	UseHubGateway  bool     `json:"useHubGateway,omitempty"`
	// ProxyCredential is an ephemeral loopback secret, never serialized to disk.
	ProxyCredential string `json:"-"`
}

type Gateway struct {
	Listen       string   `json:"listen"`
	AllowedHosts []string `json:"allowedHosts"`
	TLSCertFile  string   `json:"tlsCertFile,omitempty"`
	TLSKeyFile   string   `json:"tlsKeyFile,omitempty"`
}

type Config struct {
	// InstallationRoot is supplied by the trusted native host, never by a profile.
	InstallationRoot string `json:"-"`
	// ProfilePath is set by the local loader, never accepted from a profile.
	ProfilePath      string             `json:"-"`
	DeviceID         string             `json:"deviceId"`
	Name             string             `json:"name"`
	DataDir          string             `json:"dataDir"`
	Listen           string             `json:"listen"`
	Hub              bool               `json:"hub"`
	Executor         bool               `json:"executor"`
	HubURL           string             `json:"hubUrl,omitempty"`
	NodeTokenFile    string             `json:"nodeTokenFile,omitempty"`
	AllowedOrigins   []string           `json:"allowedOrigins"`
	AllowPrivateHTTP bool               `json:"allowPrivateHttp"`
	Projects         []protocol.Project `json:"projects"`
	Engine           Engine             `json:"engine"`
	Gateway          *Gateway           `json:"gateway,omitempty"`
}

func Load(path string) (Config, error) {
	return load(path, true)
}
func Read(path string) (Config, error) { return load(path, false) }
func load(path string, validateProjects bool) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return c, err
	}
	c.ProfilePath = filepath.Join(base, filepath.Base(path))
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	c.DataDir = resolve(c.DataDir)
	c.NodeTokenFile = resolve(c.NodeTokenFile)
	c.Engine.Home = resolve(c.Engine.Home)
	c.Engine.ProxyTokenFile = resolve(c.Engine.ProxyTokenFile)
	if c.Engine.ProxyUsername == "" {
		c.Engine.ProxyUsername = c.DeviceID
	}
	if c.DataDir == "" || c.DeviceID == "" || c.Name == "" {
		return c, errors.New("deviceId, name and dataDir are required")
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:47830"
	}
	if !c.Hub && c.HubURL == "" {
		return c, errors.New("a non-Hub device requires hubUrl")
	}
	if c.HubURL != "" {
		if err := ValidateURL(c.HubURL, c.AllowPrivateHTTP); err != nil {
			return c, err
		}
	}
	if c.Engine.UseHubGateway {
		if c.Hub || !c.Executor || c.NodeTokenFile == "" {
			return c, errors.New("engine.useHubGateway requires a paired non-Hub executor")
		}
		if c.Engine.ProxyURL != "" || c.Engine.ProxyTokenFile != "" {
			return c, errors.New("engine.useHubGateway cannot be combined with an explicit engine proxy")
		}
	}
	seen := map[string]bool{}
	for i := range c.Projects {
		p := &c.Projects[i]
		p.Path = resolve(p.Path)
		if validateProjects {
			p.Path, err = workspace.CanonicalDir(p.Path)
			if err != nil {
				return c, fmt.Errorf("project %s: %w", p.ID, err)
			}
		}
		if p.ID == "" || seen[p.ID] {
			return c, errors.New("project IDs must be unique and non-empty")
		}
		seen[p.ID] = true
	}
	if c.Gateway != nil {
		if (c.Gateway.TLSCertFile == "") != (c.Gateway.TLSKeyFile == "") {
			return c, errors.New("gateway TLS requires both a certificate and a key")
		}
		c.Gateway.TLSCertFile = resolve(c.Gateway.TLSCertFile)
		c.Gateway.TLSKeyFile = resolve(c.Gateway.TLSKeyFile)
	}
	return c, nil
}

func ValidateURL(raw string, private bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("URL must have a host and must not contain credentials")
	}
	if u.Scheme == "https" || u.Scheme == "wss" {
		return nil
	}
	if u.Scheme != "http" && u.Scheme != "ws" {
		return errors.New("unsupported URL scheme")
	}
	h := strings.ToLower(u.Hostname())
	ip := net.ParseIP(h)
	if h == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	if private && ip != nil && ip.IsPrivate() {
		return nil
	}
	return errors.New("non-loopback connections require TLS; private development IPs require allowPrivateHttp")
}
