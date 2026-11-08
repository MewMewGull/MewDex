package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func testNode(t *testing.T, endpoint string) *Node {
	t.Helper()
	state, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.Close() })
	path := filepath.Join(t.TempDir(), "node-token")
	if err = os.WriteFile(path, []byte("node-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	return &Node{Config: config.Config{HubURL: endpoint, NodeTokenFile: path}, Store: state, Device: protocol.Device{ID: "node", HostID: store.Hash("host")}}
}

func TestReconnectResetsBackoffAfterHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var attempts atomic.Int32
	accepted := make(chan time.Time, 2)
	instances := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, raw, err := conn.Read(ctx)
		var hello protocol.Frame
		if err != nil || json.Unmarshal(raw, &hello) != nil {
			return
		}
		instances <- hello.ID
		if conn.Write(ctx, websocket.MessageText, protocol.JSON(protocol.Frame{Version: protocol.Version, Type: "hello.accepted"})) != nil {
			return
		}
		accepted <- time.Now()
		if attempt == 3 {
			// Read until the executor has processed hello.accepted. A command-free
			// control frame makes that ordering explicit before disconnection.
			_ = conn.Write(ctx, websocket.MessageText, protocol.JSON(protocol.Frame{Version: protocol.Version, Type: "heartbeat.ack"}))
			_ = conn.Close(websocket.StatusGoingAway, "test disconnect")
			return
		}
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()
	node := testNode(t, server.URL)
	done := make(chan error, 1)
	go func() { done <- node.Run(ctx) }()
	var times []time.Time
	for len(times) < 2 {
		select {
		case at := <-accepted:
			times = append(times, at)
		case <-ctx.Done():
			t.Fatal("executor did not reconnect", ctx.Err())
		}
	}
	if elapsed := times[1].Sub(times[0]); elapsed > 2500*time.Millisecond {
		t.Fatalf("successful handshake did not reset the retry delay: %v", elapsed)
	}
	if first, second := <-instances, <-instances; first != second {
		t.Fatal("reconnection changed the execution process identity")
	}
	cancel()
	<-done
}

func TestRejectedCredentialIsVisibleAndReplacementReconnects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	connected := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer repaired-credential" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if _, _, err = conn.Read(ctx); err != nil {
			return
		}
		_ = conn.Write(ctx, websocket.MessageText, protocol.JSON(protocol.Frame{Version: protocol.Version, Type: "hello.accepted"}))
		connected <- struct{}{}
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()
	node := testNode(t, server.URL)
	done := make(chan error, 1)
	go func() { done <- node.Run(ctx) }()
	for {
		var status protocol.NodeConnection
		if node.Store.Get("status", "connection", &status) == nil && status.State == "pairing-required" {
			retry, err := time.Parse(time.RFC3339Nano, status.RetryAt)
			if err != nil || !retry.After(time.Now()) || status.Error == "" {
				t.Fatalf("credential rejection lost its retry details: %+v", status)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("rejected credential was not reported")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := os.WriteFile(node.Config.NodeTokenFile, []byte("repaired-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-ctx.Done():
		t.Fatal("executor did not reload its repaired credential")
	}
	cancel()
	<-done
}
