package executorpanel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"mewdex/internal/localnode"
)

func TestPanelAssetsAndExecutorManagementBoundary(t *testing.T) {
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := localnode.New(localnode.Options{Root: t.TempDir(), Program: program})
	if err != nil {
		t.Fatal(err)
	}
	handler := Handler(manager, "panel-test-token")
	for _, path := range []string{"/", "/panel.js", "/panel.css"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 200 || response.Header().Get("Location") != "" || response.Body.Len() == 0 {
			t.Fatalf("panel asset failed at %s: %d", path, response.Code)
		}
	}
	for _, test := range []struct {
		token, origin, body string
		status              int
	}{
		{"", "", `{"operation":"status"}`, 401},
		{"panel-test-token", "https://other.example", `{"operation":"status"}`, 403},
		{"panel-test-token", "", `{"operation":"status"}`, 200},
		{"panel-test-token", "", `{"operation":"setup","setup":{"hub":true,"executor":true}}`, 400},
		{"panel-test-token", "", `{"operation":"model-routing.apply"}`, 400},
		{"panel-test-token", "", `{"operation":"start"}`, 409},
	} {
		request := httptest.NewRequest("POST", "http://localhost/api/device", strings.NewReader(test.body))
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		request.Header.Set("Origin", test.origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("panel request %s returned %d: %s", test.body, response.Code, response.Body.String())
		}
		if response.Code == http.StatusOK && !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
			t.Fatal("panel API lost its JSON content type")
		}
	}
}
