package localnode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/serviceport"
	"mewdex/internal/startup"
)

func TestExistingHubIsDiscoveredWithoutStartingOrAdoptingAnotherInstallation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" || r.Header.Get("Authorization") != "" {
			t.Error("discovery attempted authentication or another operation")
		}
		_ = json.NewEncoder(w).Encode(protocol.ServerInfo{Name: "MewDex", ProtocolVersion: protocol.Version})
	}))
	defer server.Close()
	m := testManager(t)
	m.options.StartupBackend = &startupBackendFixture{}
	ctx := context.Background()
	setup, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true, Listen: strings.TrimPrefix(server.URL, "http://")}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(m.profile())
	for _, operation := range []string{"hub.discover", "start"} {
		result, err := m.Handle(ctx, Request{Operation: operation, ExpectedDeviceID: setup.Snapshot.DeviceID})
		if err != nil || result.ExistingHub == nil || result.ExistingHub.BaseURL != server.URL {
			t.Fatalf("did not discover running Hub: %+v %v", result.ExistingHub, err)
		}
		if result.Snapshot.Ready || result.Snapshot.State != "stopped" || result.Connection != nil {
			t.Fatal("discovery adopted the foreign service or granted credentials")
		}
	}
	_, err = m.Handle(ctx, Request{Operation: "startup.apply", Startup: &StartupRequest{Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(m.options.Root + "/startup.json")
	var record startup.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	driver, err := New(Options{Root: m.options.Root, Program: record.Definition.Program, StartupBackend: m.options.StartupBackend})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := driver.RunManaged(ctx, record.Definition.Owner, func(context.Context, config.Config) error { called = true; return nil }); err != nil || called {
		t.Fatal("login startup created a second Hub on an existing Hub's port", err)
	}
	after, _ := os.ReadFile(m.profile())
	if string(before) != string(after) {
		t.Fatal("discovery changed the user's selected profile or port")
	}
}

func TestOtherServicesAndRedirectsRemainPortConflicts(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "other-service", true: "redirect"}[redirect], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if redirect {
					http.Redirect(w, r, "http://127.0.0.1:1/health", http.StatusFound)
					return
				}
				_, _ = w.Write([]byte(`{"name":"AnotherService","protocolVersion":1}`))
			}))
			defer server.Close()
			m := testManager(t)
			ctx := context.Background()
			if _, err := m.Handle(ctx, Request{Operation: "setup", Setup: &Setup{Hub: true, Listen: strings.TrimPrefix(server.URL, "http://")}}); err != nil {
				t.Fatal(err)
			}
			_, err := m.Handle(ctx, Request{Operation: "start"})
			var occupied *serviceport.OccupiedError
			if !errors.As(err, &occupied) {
				t.Fatal("non-Hub service was accepted as a Hub", err)
			}
		})
	}
}
