package webinput

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"mewdex/internal/inputbridge"
	"mewdex/internal/protocol"
)

func TestConnectionSurvivesRestartAndOnlyItsOwnerCanDisconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_ = json.NewEncoder(w).Encode(protocol.ServerInfo{ProtocolVersion: protocol.Version, HubDeviceID: "fixture-hub"})
		case "/v1/me":
			_ = json.NewEncoder(w).Encode(protocol.Identity{ID: "fixture-owner", Role: "admin"})
		default:
			http.Error(w, "offline", 503)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	channels, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	options := inputbridge.Options{BaseURL: server.URL, Token: "fixture-private-credential", HubDeviceID: "fixture-hub", IdentityID: "fixture-owner", Name: "Web fixture"}
	if err = channels.Configure(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if err = channels.Configure(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if err = channels.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "channel-connection"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && bytes.Contains(raw, []byte(options.Token)) {
		t.Fatal("Windows connection was stored without user-bound encryption")
	}
	restored, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.Connection() != options || !restored.Connected() {
		t.Fatal("restart did not restore the background connection")
	}
	if err = restored.Disconnect("another-account"); err == nil {
		t.Fatal("another account stopped the connection")
	}
	if err = restored.Disconnect(options.Token); err != nil {
		t.Fatal(err)
	}
	if restored.Connected() || restored.Connection().Token != "" {
		t.Fatal("disconnect left its connection running")
	}
	if _, err = os.Stat(filepath.Join(root, "channel-connection")); !os.IsNotExist(err) {
		t.Fatal("disconnect left saved credentials")
	}
}

func TestPublicIdentityMismatchNeverReceivesCredentials(t *testing.T) {
	var credentials atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			credentials.Add(1)
		}
		_ = json.NewEncoder(w).Encode(protocol.ServerInfo{ProtocolVersion: protocol.Version, HubDeviceID: "wrong-hub"})
	}))
	defer server.Close()
	channels, err := New(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer channels.Close()
	err = channels.Configure(context.Background(), inputbridge.Options{BaseURL: server.URL, Token: "fixture-private", HubDeviceID: "expected-hub", IdentityID: "owner"})
	if err == nil || credentials.Load() != 0 || channels.Connected() {
		t.Fatal("an unverified Hub received a connection credential")
	}
}
