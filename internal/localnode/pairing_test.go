package localnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/hub"
	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"mewdex/internal/transport"
)

func repairFixture(t *testing.T, base string, token []byte) (*Manager, config.Config) {
	t.Helper()
	m := testManager(t)
	c := config.Config{DeviceID: "executor-device", Name: "executor", Executor: true, HubURL: base, NodeTokenFile: filepath.Join(m.options.Root, "node-token"), DataDir: filepath.Join(m.options.Root, "data"), Projects: []protocol.Project{{ID: "project", Path: t.TempDir()}}, Engine: config.Engine{UseHubGateway: true}}
	if token != nil {
		if err := createFile(c.NodeTokenFile, token); err != nil {
			t.Fatal(err)
		}
	}
	if err := createJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	return m, c
}

func repairRequest(code string) Request {
	return Request{Operation: "repair.executor", ExpectedDeviceID: "executor-device", Pairing: &PairingRequest{Invitation: code}}
}

func fixtureExecutorIdentity() protocol.Identity {
	return protocol.Identity{ID: "new-identity", Role: "executor", DeviceID: "executor-device", Access: protocol.Access{Gateway: true}}
}

func requireFile(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("retained file changed or cannot be read: %s", filepath.Base(path))
	}
}

type refusedPairingCommand struct{ calls atomic.Int32 }

func (h *refusedPairingCommand) Handle(_ context.Context, c protocol.Command) protocol.CommandResult {
	h.calls.Add(1)
	return protocol.CommandResult{CommandID: c.ID, State: "failed", Error: "pairing probe refuses execution"}
}

func TestExecutorRepairsRejectedCredentialAfterHubRestore(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	invite := func() string {
		t.Helper()
		code, err := s.InviteWithAccess("executor", protocol.Access{Gateway: true}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	oldToken, _, err := s.Pair(invite(), "executor", "executor-device")
	if err != nil {
		t.Fatal(err)
	}
	var pairs atomic.Int32
	handler := hub.New(config.Config{DeviceID: "hub"}, s).Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/pair" {
			pairs.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	previous := []byte(oldToken + "\r\n")
	m, c := repairFixture(t, server.URL, previous)
	profile, _ := os.ReadFile(m.profile())
	if err = s.Put("sessions", "retained", protocol.Session{ID: "retained", DeviceID: c.DeviceID, State: "running"}); err != nil {
		t.Fatal(err)
	}
	if err = s.PrepareRestore(ctx, "hub", false); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(ctx, repairRequest("")); err == nil {
		t.Fatal("restored Hub accepted its previous executor identity")
	}
	status, err := m.Handle(ctx, Request{Operation: "status"})
	if err != nil || !status.Snapshot.PairingRequired {
		t.Fatal("rejected authorization was not exposed to the native caller")
	}
	result, err := m.Handle(ctx, repairRequest(invite()))
	if err != nil || result.Snapshot.PairingRequired || result.Snapshot.DeviceID != c.DeviceID {
		t.Fatalf("executor could not reconnect with its original device ID: %v", err)
	}
	backups, _ := filepath.Glob(c.NodeTokenFile + ".rejected.*")
	if len(backups) != 1 {
		t.Fatal("previous credential was not retained exactly once")
	}
	requireFile(t, backups[0], previous)
	requireFile(t, m.profile(), profile)
	active, _ := os.ReadFile(c.NodeTokenFile)
	identity, err := s.Authenticate(string(active))
	if err != nil || identity.DeviceID != c.DeviceID || pairs.Load() != 1 {
		t.Fatal("replacement identity was not adopted exactly once")
	}
	unused := invite()
	if _, err = m.Handle(ctx, repairRequest(unused)); err != nil || pairs.Load() != 1 {
		t.Fatal("valid credential consumed another invitation")
	}
	if _, _, err = s.Pair(unused, "another executor", "another-device"); err != nil {
		t.Fatal("valid-identity retry consumed the supplied invitation")
	}
	var session protocol.Session
	if err = s.Get("sessions", "retained", &session); err != nil || !session.Frozen || session.State != "unknown" {
		t.Fatal("network repair altered the restored task's frozen unknown outcome")
	}
	// Rejoin through the production WebSocket transport and acknowledge a labelled
	// diagnostic event. The handler refuses execution; this performs no model turn.
	nodeStore, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	if _, err = nodeStore.Append(protocol.Event{ID: "repaired-event", DeviceID: c.DeviceID, Kind: "development.pairing-probe", Payload: protocol.JSON(map[string]bool{"synthetic": true}), CreatedAt: protocol.Now()}); err != nil {
		t.Fatal(err)
	}
	nodeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	handlerProbe := &refusedPairingCommand{}
	node := &transport.Node{Config: c, Store: nodeStore, Device: protocol.Device{ID: c.DeviceID, Name: c.Name, Roles: []string{"executor"}, ProtocolVersion: protocol.Version}, Executor: handlerProbe}
	done := make(chan error, 1)
	go func() { done <- node.Run(nodeContext) }()
	defer func() { cancel(); <-done }()
	for {
		pending, err := nodeStore.Unacked()
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-nodeContext.Done():
			t.Fatal("repaired executor did not rejoin and acknowledge its diagnostic event")
		case <-time.After(10 * time.Millisecond):
		}
	}
	events, err := s.Events(0, "", 100)
	if err != nil || len(events) != 1 || events[0].ID != "repaired-event" || handlerProbe.calls.Load() != 0 {
		t.Fatal("transport recovery duplicated an event or attempted execution")
	}
}

func TestExecutorDoesNotReplaceOnUncertainOrUnrelatedAuthorizationFailures(t *testing.T) {
	for _, kind := range []string{"offline", "forbidden", "server-error", "wrong-device", "gateway-missing", "valid"} {
		t.Run(kind, func(t *testing.T) {
			var pairs atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/pair" {
					pairs.Add(1)
				}
				identity := fixtureExecutorIdentity()
				switch kind {
				case "forbidden":
					w.WriteHeader(403)
					return
				case "server-error":
					w.WriteHeader(503)
					return
				case "wrong-device":
					identity.DeviceID = "another-device"
				case "gateway-missing":
					identity.Access.Gateway = false
				}
				_ = json.NewEncoder(w).Encode(identity)
			}))
			defer server.Close()
			previous := []byte("original-executor-token\n")
			m, c := repairFixture(t, server.URL, previous)
			if kind == "offline" {
				server.Close()
			}
			_, err := m.Handle(context.Background(), repairRequest("unused-new-invitation"))
			if (kind == "valid") != (err == nil) || pairs.Load() != 0 {
				t.Fatal("an unrelated failure or valid identity triggered pairing")
			}
			requireFile(t, c.NodeTokenFile, previous)
			if m.describe(c).PairingRequired {
				t.Fatal("non-401 condition was treated as a rejected identity")
			}
		})
	}
}

func TestExecutorResumesSavedPairingWithoutAnotherInvitation(t *testing.T) {
	var ready atomic.Bool
	var pairs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/pair" {
			pairs.Add(1)
			_ = json.NewEncoder(w).Encode(Credentials{Token: "replacement-token", Identity: fixtureExecutorIdentity(), ProtocolVersion: protocol.Version})
			return
		}
		if r.Header.Get("Authorization") == "Bearer replacement-token" {
			if !ready.Load() {
				w.WriteHeader(503)
				return
			}
			_ = json.NewEncoder(w).Encode(fixtureExecutorIdentity())
			return
		}
		w.WriteHeader(401)
	}))
	defer server.Close()
	previous := []byte("previous-token\n")
	m, c := repairFixture(t, server.URL, previous)
	if _, err := m.Handle(context.Background(), repairRequest("one-time-secret-invitation")); err == nil {
		t.Fatal("unverified replacement was adopted")
	}
	requireFile(t, c.NodeTokenFile, previous)
	raw, err := os.ReadFile(c.NodeTokenFile + ".pairing.json")
	if err != nil || bytes.Contains(raw, []byte("one-time-secret-invitation")) || !bytes.Contains(raw, []byte("replacement-token")) {
		t.Fatal("saved pairing result was lost or retained the invitation")
	}
	ready.Store(true)
	if _, err = m.Handle(context.Background(), repairRequest("")); err != nil || pairs.Load() != 1 {
		t.Fatalf("saved result did not resume independently: %v", err)
	}
	requireFile(t, c.NodeTokenFile, []byte("replacement-token"))
	if _, err = os.Stat(c.NodeTokenFile + ".pairing.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("adopted pending result was not retired")
	}
}

func TestExecutorDoesNotReplayAnUnconfirmedPairingRequest(t *testing.T) {
	var pairs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pair" {
			if r.Header.Get("Authorization") == "Bearer replacement-token" {
				_ = json.NewEncoder(w).Encode(fixtureExecutorIdentity())
			} else {
				w.WriteHeader(401)
			}
			return
		}
		if pairs.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"token":`)) // Lost response after an unknown server outcome.
			return
		}
		_ = json.NewEncoder(w).Encode(Credentials{Token: "replacement-token", Identity: fixtureExecutorIdentity(), ProtocolVersion: protocol.Version})
	}))
	defer server.Close()
	previous := []byte("previous-token")
	m, c := repairFixture(t, server.URL, previous)
	for i := 0; i < 2; i++ {
		if _, err := m.Handle(context.Background(), repairRequest("first-invitation")); err == nil || !strings.Contains(err.Error(), "unconfirmed") {
			t.Fatal("unknown pairing was treated as a repeatable request")
		}
	}
	if pairs.Load() != 1 {
		t.Fatal("same invitation was submitted after an unknown result")
	}
	requireFile(t, c.NodeTokenFile, previous)
	if _, err := m.Handle(context.Background(), repairRequest("explicitly-different-invitation")); err != nil || pairs.Load() != 2 {
		t.Fatalf("new explicit attempt did not proceed: %v", err)
	}
	retained, _ := filepath.Glob(c.NodeTokenFile + ".pairing.json.unconfirmed.*")
	if len(retained) != 1 {
		t.Fatal("unknown previous attempt was discarded")
	}
	raw, _ := os.ReadFile(retained[0])
	if bytes.Contains(raw, []byte("first-invitation")) {
		t.Fatal("unknown attempt retained its raw invitation")
	}
}

func TestExecutorRecoversExitAfterCredentialReplacement(t *testing.T) {
	var pairs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/pair" {
			pairs.Add(1)
		}
		_ = json.NewEncoder(w).Encode(fixtureExecutorIdentity())
	}))
	defer server.Close()
	m, c := repairFixture(t, server.URL, []byte("replacement-token"))
	attempt := executorPairingAttempt{Format: 1, ID: protocol.ID(), HubURL: c.HubURL, DeviceID: c.DeviceID, PreviousSHA256: credentialHash([]byte("previous-token")), InvitationHash: credentialHash([]byte("consumed-invitation")), Credentials: &Credentials{Token: "replacement-token", Identity: fixtureExecutorIdentity(), ProtocolVersion: protocol.Version}}
	if err := createJSON(c.NodeTokenFile+".pairing.json", attempt); err != nil {
		t.Fatal(err)
	}
	if err := createFile(c.NodeTokenFile+".rejected."+attempt.ID, []byte("previous-token")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Handle(context.Background(), repairRequest("unused-invitation")); err != nil || pairs.Load() != 0 {
		t.Fatal("already adopted identity was paired again")
	}
	if _, err := os.Stat(c.NodeTokenFile + ".pairing.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed replacement retained a pending journal")
	}
	requireFile(t, c.NodeTokenFile+".rejected."+attempt.ID, []byte("previous-token"))
}

func TestExecutorRepairPreservesConcurrentCredentialChange(t *testing.T) {
	var tokenPath string
	var pairs atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/pair" {
			pairs.Add(1)
			if err := os.WriteFile(tokenPath, []byte("separately-changed-token"), 0600); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(Credentials{Token: "replacement-token", Identity: fixtureExecutorIdentity(), ProtocolVersion: protocol.Version})
			return
		}
		if r.Header.Get("Authorization") == "Bearer replacement-token" {
			_ = json.NewEncoder(w).Encode(fixtureExecutorIdentity())
		} else {
			w.WriteHeader(401)
		}
	}))
	defer server.Close()
	m, c := repairFixture(t, server.URL, []byte("previous-token"))
	tokenPath = c.NodeTokenFile
	if _, err := m.Handle(context.Background(), repairRequest("one-time-invitation")); err == nil {
		t.Fatal("concurrent credential modification was overwritten")
	}
	requireFile(t, tokenPath, []byte("separately-changed-token"))
	if _, err := m.Handle(context.Background(), repairRequest("another-invitation")); err == nil || pairs.Load() != 1 {
		t.Fatal("mismatched pending state was silently superseded")
	}
}

func TestExecutorRepairRequiresStoppedOwnedInstallation(t *testing.T) {
	m, c := repairFixture(t, "http://127.0.0.1:1", []byte("previous-token"))
	lock, err := lifecycle.Acquire(c.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(context.Background(), repairRequest("invitation")); err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatal("running executor allowed an identity change")
	}
	lock.Close()
	external := filepath.Join(t.TempDir(), "node-token")
	if err = createFile(external, []byte("external-token")); err != nil {
		t.Fatal(err)
	}
	c.NodeTokenFile = external
	if err = replaceJSON(m.profile(), c); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Handle(context.Background(), repairRequest("invitation")); err == nil {
		t.Fatal("repair accepted an external credential path")
	}
	requireFile(t, external, []byte("external-token"))
}
