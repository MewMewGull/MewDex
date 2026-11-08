package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHubProxyUsesConfiguredVirtualHostAndPreservesBrowserRequest(t *testing.T) {
	var upstreamHost string
	observed := make(chan *http.Request, 1)
	bodies := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != upstreamHost {
			http.Error(w, "wrong virtual host", http.StatusMisdirectedRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read request", http.StatusBadRequest)
			return
		}
		observed <- r
		bodies <- string(body)
		_, _ = io.WriteString(w, "HUB_OK")
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL + "/gateway")
	if err != nil {
		t.Fatal(err)
	}
	upstreamHost = target.Host
	request := httptest.NewRequest(http.MethodPost, "https://workbench.example.test/v1/query?limit=1", strings.NewReader("request-body"))
	request.Header.Set("Authorization", "Bearer fixture-credential")
	request.Header.Set("Origin", "http://127.0.0.1:47831")
	response := httptest.NewRecorder()
	newHubProxy(target).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "HUB_OK" {
		t.Fatalf("configured Hub was not reached through its virtual host: %d %s", response.Code, response.Body.String())
	}
	actual := <-observed
	if actual.Method != http.MethodPost || actual.URL.RequestURI() != "/gateway/v1/query?limit=1" || <-bodies != "request-body" {
		t.Fatal("proxy changed the browser method, path, query or body")
	}
	if actual.Header.Get("Authorization") != "Bearer fixture-credential" || actual.Header.Get("Origin") != "http://127.0.0.1:47831" {
		t.Fatal("proxy changed the browser credentials or Origin")
	}
}
