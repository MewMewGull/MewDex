package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/hub"
	"mewdex/internal/inputbridge"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestStandaloneSetupPersistsIssuedKeyAndPinsHub(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	login := protocol.MewDexAccountLogin{Username: "connector-user", Password: "fixture-password"}
	if err := s.ProvisionMewDexAccount(login); err != nil {
		t.Fatal(err)
	}
	h := hub.New(config.Config{DeviceID: "setup-hub"}, s)
	server := httptest.NewServer(h.Handler())
	defer server.Close()
	path := filepath.Join(t.TempDir(), "connector", "connection.json")
	if err := configure(context.Background(), path, server.URL, "Always-on connector", bytes.NewReader(protocol.JSON(login))); err != nil {
		t.Fatal(err)
	}
	connection, err := readConnection(path)
	if err != nil || connection.Token == "" || connection.HubDeviceID != "setup-hub" || connection.Name != "Always-on connector" {
		t.Fatal("setup did not save the verified connection", err)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte(login.Password)) {
		t.Fatal("setup saved the account password")
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("connector credential file is not private")
	}
	// Re-authorization retains the same identity and therefore the same data partition.
	if err := configure(context.Background(), path, server.URL, "", bytes.NewReader(protocol.JSON(login))); err != nil {
		t.Fatal(err)
	}
	restored, _ := readConnection(path)
	if restored.IdentityID != connection.IdentityID || restored.Name != connection.Name {
		t.Fatal("re-authorization changed the connector identity")
	}
	loginRequests := 0
	changed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/login" {
			loginRequests++
		}
		_ = json.NewEncoder(w).Encode(protocol.ServerInfo{HubDeviceID: "different-hub", ProtocolVersion: protocol.Version})
	}))
	defer changed.Close()
	if configure(context.Background(), path, changed.URL, "", bytes.NewReader(protocol.JSON(login))) == nil || loginRequests != 0 {
		t.Fatal("setup sent account credentials to a changed Hub")
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(unchanged, protocol.JSON(restored)) && strings.TrimSpace(string(unchanged)) != string(protocol.JSON(restored)) {
		t.Fatal("failed setup replaced the existing connection")
	}
}

func TestStandaloneProcessFixture(t *testing.T) {
	if os.Getenv("MEWDEX_CHANNEL_PROCESS_FIXTURE") != "1" {
		return
	}
	if err := run([]string{"-config", os.Getenv("MEWDEX_CHANNEL_FIXTURE_CONFIG")}, os.Stdin); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneSurvivesClosedInputAndAcceptsHubAdministratorManagement(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := hub.New(config.Config{DeviceID: "standalone-hub"}, s)
	server := httptest.NewServer(h.Handler())
	defer server.Close()
	code, _ := s.Invite("admin", time.Minute)
	token, owner, _ := s.Pair(code, "connector-owner", "")
	code, _ = s.Invite("admin", time.Minute)
	controller, _, _ := s.Pair(code, "controller-on-another-input", "")
	path := filepath.Join(t.TempDir(), "connection.json")
	options := inputbridge.Options{BaseURL: server.URL, Token: token, HubDeviceID: "standalone-hub", IdentityID: owner.ID, Name: "Independent fixture"}
	if err := os.WriteFile(path, protocol.JSON(options), 0600); err != nil {
		t.Fatal(err)
	}
	process := exec.Command(os.Args[0], "-test.run=^TestStandaloneProcessFixture$")
	process.Env = append(os.Environ(), "MEWDEX_CHANNEL_PROCESS_FIXTURE=1", "MEWDEX_CHANNEL_FIXTURE_CONFIG="+path)
	// A nil Stdin gives the child EOF immediately, as a service manager would.
	var output bytes.Buffer
	process.Stdout, process.Stderr = &output, &output
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	defer func() { _ = process.Process.Kill(); <-done }()
	client := &http.Client{Timeout: time.Second}
	var connector protocol.InputBridge
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			// Preserve a completion value for the cleanup above.
			done <- err
			t.Fatalf("standalone stopped when standard input closed: %v %s", err, output.String())
		default:
		}
		request, _ := http.NewRequest("GET", server.URL+"/v1/input-bridges", nil)
		request.Header.Set("Authorization", "Bearer "+controller)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var bridges []protocol.InputBridge
		err = json.NewDecoder(response.Body).Decode(&bridges)
		response.Body.Close()
		if err == nil && len(bridges) == 1 {
			connector = bridges[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if connector.Kind != "standalone" {
		t.Fatal("independent connector was not visible to the Hub administrator")
	}
	secret := "standalone-platform-secret"
	request, _ := http.NewRequest("PUT", server.URL+"/v1/messaging/qq?bridgeId="+connector.ID, bytes.NewReader(protocol.JSON(protocol.MessagingUpdate{AppID: "standalone-bot", Secret: &secret})))
	request.Header.Set("Authorization", "Bearer "+controller)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var views []protocol.MessagingChannel
	if json.NewDecoder(response.Body).Decode(&views) != nil || response.StatusCode != 200 || len(views) != 3 || views[1].AppID != "standalone-bot" || !views[1].CredentialConfigured {
		t.Fatal("Hub administrator could not manage the independent service", response.Status)
	}
}
