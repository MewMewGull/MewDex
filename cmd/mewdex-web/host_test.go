package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mewdex/internal/webinput"
)

func TestLocalSettingsAndSessionRequireTheTrayKeyAndLoopbackOrigin(t *testing.T) {
	root := t.TempDir()
	channels, err := webinput.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer channels.Close()
	host := &webHost{root: root, key: "fixture-panel-key", channels: channels}
	for _, item := range []struct {
		path, token, remote, origin string
		code                        int
	}{
		{"/__mewdex/config", "", "127.0.0.1:1", "", 403},
		{"/__mewdex/config", "fixture-panel-key", "192.0.2.1:1", "", 403},
		{"/__mewdex/config", "fixture-panel-key", "127.0.0.1:1", "http://other.example", 403},
		{"/__mewdex/config", "fixture-panel-key", "127.0.0.1:1", "http://localhost", 200},
		{"/__mewdex/session", "", "127.0.0.1:1", "", 403},
		{"/__mewdex/session", "fixture-panel-key", "127.0.0.1:1", "", 200},
	} {
		request := httptest.NewRequest("GET", "http://localhost"+item.path, nil)
		request.RemoteAddr = item.remote
		request.Header.Set("Origin", item.origin)
		request.Header.Set("Authorization", "Bearer "+item.token)
		response := httptest.NewRecorder()
		host.ServeHTTP(response, request)
		if response.Code != item.code {
			t.Fatalf("%s from %s got %d", item.path, item.remote, response.Code)
		}
	}
}

func TestUnconfiguredInputAndHostedChannelMetadata(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("<head></head>"), 0600); err != nil {
		t.Fatal(err)
	}
	host := &webHost{settings: webSettings{Directory: directory}}
	response := httptest.NewRecorder()
	host.ServeHTTP(response, httptest.NewRequest("GET", "http://localhost/", nil))
	if response.Code != 303 || response.Header().Get("Location") != "/__mewdex/settings" {
		t.Fatal("initial launch does not lead to connection setup")
	}
	host.settings.HubURL = "https://hub.example.test"
	response = httptest.NewRecorder()
	host.ServeHTTP(response, httptest.NewRequest("GET", "http://localhost/", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `name="mewdex-input-channels" content="hosted"`) || !strings.Contains(response.Body.String(), `content="https://hub.example.test"`) {
		t.Fatal("hosted input is missing its channel capability or actual Hub address")
	}
}

func TestLateChannelRefreshCannotUndoSignOut(t *testing.T) {
	root := t.TempDir()
	channels, err := webinput.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer channels.Close()
	host := &webHost{channels: channels, channelEpoch: map[string]uint64{"fixture-tab": 3}}
	request := httptest.NewRequest("POST", "http://localhost/__mewdex/channels", strings.NewReader(`{"operation":"connect","clientId":"fixture-tab","epoch":2}`))
	request.Header.Set("Authorization", "Bearer fixture-token")
	response := httptest.NewRecorder()
	host.ServeHTTP(response, request)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "stale") || channels.Connected() {
		t.Fatal("late refresh undid a newer disconnect")
	}
}
