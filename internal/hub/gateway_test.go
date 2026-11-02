package hub

import (
	"context"
	"net/http"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/gateway"
	"mewdex/internal/protocol"
)

func TestHubModelTunnelRequiresExplicitExecutorGatewayGrant(t *testing.T) {
	h, s, server, admin := testHub(t)
	g, err := gateway.New(config.Gateway{AllowedHosts: []string{"chatgpt.com"}}, h.AuthorizeGateway)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	h.SetModelGateway(g)
	if response := request(t, server.URL, admin, "GET", "/v1/gateway/ws?target=denied:443", nil); response.StatusCode != 403 {
		t.Fatal("admin became a gateway identity")
	}
	code, err := s.InviteWithAccess("executor", protocol.Access{Gateway: true}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, id, err := s.Pair(code, "remote", "server")
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, server.URL, token, "GET", "/v1/gateway/ws?target=denied:443", nil); response.StatusCode != 403 {
		t.Fatalf("forbidden target: %d", response.StatusCode)
	}
	if err = s.UpdateAccess(id.ID, protocol.Access{}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.AuthorizeGateway(context.Background(), "server", token); err == nil {
		t.Fatal("removed grant remained authorized")
	}
	response := request(t, server.URL, token, http.MethodGet, "/v1/gateway/ws?target=chatgpt.com:443", nil)
	if response.StatusCode != 403 {
		t.Fatal("gateway route ignored revoked scope")
	}
}
