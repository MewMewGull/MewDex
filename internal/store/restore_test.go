package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func TestRestoredStateCannotRevivePermissionsOrResumeWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	code, err := s.Invite("admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := s.Pair(code, "input", "")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.Invite("client", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Put("sessions", "session", protocol.Session{ID: "session", DeviceID: "device", State: "running", TurnID: "old-turn", ExecutionScopeID: "old-installation-scope"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("queue_items", "item", protocol.QueueItem{ID: "item", DeviceID: "device", SessionID: "session", State: "running", Revision: 2}); err != nil {
		t.Fatal(err)
	}
	if err = s.Put("interactions", "request", protocol.Interaction{ID: "request", SessionID: "session", State: "pending"}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"session.updated", "interaction.updated", "queue.item.updated", "message.user"} {
		if _, err = s.Append(protocol.Event{ID: protocol.ID(), DeviceID: "device", SessionID: "session", Kind: kind, Payload: protocol.JSON(map[string]string{"state": "old"})}); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if err = ValidateSnapshot(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.PrepareRestore(context.Background(), "device", true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(token); err == nil {
		t.Fatal("old identity was reactivated")
	}
	if _, _, err = s.Pair(pending, "new-input", ""); err == nil {
		t.Fatal("old invitation was reactivated")
	}
	var session protocol.Session
	if err = s.Get("sessions", "session", &session); err != nil || session.State != "unknown" || !session.Frozen || session.TurnID != "old-turn" {
		t.Fatal("restored task lost its unknown execution state")
	}
	var scopeRestore map[string]string
	if session.ExecutionScopeID != "" || s.Get("execution_scope_restore", session.ID, &scopeRestore) != nil || scopeRestore["previousScopeId"] != "old-installation-scope" {
		t.Fatal("restored session reused an old installation scope or lost the audit record")
	}
	var item protocol.QueueItem
	if err = s.Get("queue_items", "item", &item); err != nil || item.State != "unknown" || item.Revision != 3 {
		t.Fatal("restored queue became executable")
	}
	var interaction protocol.Interaction
	if err = s.Get("interactions", "request", &interaction); err != nil || interaction.State != "expired" {
		t.Fatal("old callback remained answerable")
	}
	pendingEvents, err := s.Unacked()
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingEvents) != 1 || pendingEvents[0].Kind != "message.user" {
		t.Fatal("obsolete state could be replayed, or original message evidence was discarded")
	}
}

func TestHubIndexRestoreDoesNotRebindOtherExecutorsScopes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Put("sessions", "remote", protocol.Session{ID: "remote", DeviceID: "another-device", State: "idle", ExecutionScopeID: "remote-scope"}); err != nil {
		t.Fatal(err)
	}
	if err = s.PrepareRestore(context.Background(), "hub", false); err != nil {
		t.Fatal(err)
	}
	var session protocol.Session
	if err = s.Get("sessions", "remote", &session); err != nil || session.ExecutionScopeID != "remote-scope" || !session.Frozen {
		t.Fatal("hub restore rewrote remote execution ownership", err)
	}
}

func TestSnapshotValidationDoesNotCreateOrRepairADatabase(t *testing.T) {
	for _, content := range [][]byte{nil, []byte("not a database")} {
		path := filepath.Join(t.TempDir(), "state.db")
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
		if err := ValidateSnapshot(context.Background(), path); err == nil {
			t.Fatal("invalid database was accepted")
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(content) {
			t.Fatal("validation changed the input file")
		}
	}
}
