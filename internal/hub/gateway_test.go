package hub

import (
	"net/http"
	"testing"
)

func TestLegacyModelGatewayRoutesAreRemoved(t *testing.T) {
	_, _, server, admin := testHub(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/v1/gateway"},
		{http.MethodPost, "/v1/gateway/authorize"},
		{http.MethodGet, "/v1/gateway/ws"},
		{http.MethodGet, "/v1/gateway/relay/ws"},
	} {
		response := request(t, server.URL, admin, call.method, call.path, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("obsolete gateway route remains active %s %s %d", call.method, call.path, response.StatusCode)
		}
	}
}
