package hub

import (
	"net/http"
	"net/http/httptest"
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

func TestHubOnlyServesItsAPI(t *testing.T) {
	h, _, _, _ := testHub(t)
	handler := h.Handler()
	for _, path := range []string{"/", "/index.html", "/assets/app.js"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("Hub served input assets at %s", path)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatal("Hub API is unavailable")
	}
}
