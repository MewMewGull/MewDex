package localnode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

type HubDiscovery struct {
	BaseURL         string `json:"baseUrl"`
	ProtocolVersion int    `json:"protocolVersion"`
}

type existingHubError struct{ Hub *HubDiscovery }

func (e *existingHubError) Error() string {
	return fmt.Sprintf("此端口已有 MewDex Hub，可连接 %s 并登录。", e.Hub.BaseURL)
}

// Read public discovery only. Finding a Hub never grants its credentials or
// adopts its installation, process, device identity or startup registration.
func discoverHub(ctx context.Context, c config.Config) *HubDiscovery {
	if !c.Hub {
		return nil
	}
	host, rawPort, err := net.SplitHostPort(c.Listen)
	port, parseErr := strconv.Atoi(rawPort)
	if err != nil || parseErr != nil || port < 1 || port > 65535 {
		return nil
	}
	local := "127.0.0.1"
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		local = "::1"
	}
	base := "http://" + net.JoinHostPort(local, rawPort)
	ctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return nil
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	var info protocol.ServerInfo
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&info) != nil || info.Name != "MewDex" || info.ProtocolVersion < 1 {
		return nil
	}
	return &HubDiscovery{BaseURL: base, ProtocolVersion: info.ProtocolVersion}
}
