package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBundledBrowserOriginsStillRequireAuthentication(t *testing.T) {
	h, _, _, _ := testHub(t)
	publicOrigin := "https://workbench.example.com"
	h.SetBrowserOrigins(func() []string { return []string{"http://127.0.0.1:47830", publicOrigin} })
	handler := h.Handler()
	for _, origin := range []string{"http://127.0.0.1:47830", publicOrigin, "https://unconfigured.example.com"} {
		r := httptest.NewRequest("GET", "/health", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		expected := http.StatusOK
		if origin == "https://unconfigured.example.com" {
			expected = http.StatusForbidden
		}
		if w.Code != expected {
			t.Fatalf("origin %s returned %d", origin, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/v1/me", nil)
	r.Header.Set("Origin", publicOrigin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("web UI bypassed API authentication")
	}
	publicOrigin = "https://replacement.example.com"
	r = httptest.NewRequest("OPTIONS", "/v1/login", nil)
	r.Header.Set("Origin", publicOrigin)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != publicOrigin {
		t.Fatal("updated public origin was not adopted")
	}
}

func TestWebEntryCanStopWithoutStoppingTheHub(t *testing.T) {
	h, _, _, _ := testHub(t)
	enabled := false
	h.SetWebUI(func() bool { return enabled })
	handler := h.Handler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "尚未启动") {
		t.Fatal("web entry was enabled by default")
	}
	enabled = true
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatal("Hub stopped with its web entry")
	}
	enabled = false
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/index.html", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("stopped web assets remained available")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatal("stopping the web entry stopped the Hub")
	}
}
