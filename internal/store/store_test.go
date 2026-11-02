package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func memory(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReplayDoesNotRollSessionBack(t *testing.T) {
	s := memory(t)
	session := protocol.Session{ID: "session", DeviceID: "node", State: "running"}
	first := protocol.Event{ID: "one", DeviceID: "node", SessionID: "session", Kind: "session.updated", Payload: protocol.JSON(session)}
	if _, err := s.Ingest(first); err != nil {
		t.Fatal(err)
	}
	session.State = "idle"
	second := first
	second.ID = "two"
	second.Payload = protocol.JSON(session)
	if _, err := s.Ingest(second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(first); err != nil {
		t.Fatal(err)
	}
	var actual protocol.Session
	if err := s.Get("sessions", "session", &actual); err != nil {
		t.Fatal(err)
	}
	if actual.State != "idle" {
		t.Fatalf("old event rolled state back: %s", actual.State)
	}
	events, err := s.Events(0, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("duplicate event persisted: %d", len(events))
	}
}

func TestIdempotencyBindsPayloadAndSource(t *testing.T) {
	s := memory(t)
	c := protocol.Command{ID: "command", DeviceID: "node", SourceID: "alice", Kind: "start", CreatedAt: "first"}
	if _, owner, err := s.Claim(c); err != nil || !owner {
		t.Fatalf("initial claim: %v %v", owner, err)
	}
	c.CreatedAt = "retry"
	if _, owner, err := s.Claim(c); err != nil || owner {
		t.Fatalf("retry claim: %v %v", owner, err)
	}
	c.SourceID = "bob"
	if _, _, err := s.Claim(c); !errors.Is(err, ErrConflict) {
		t.Fatalf("another source reused command: %v", err)
	}
}

func TestPairingCodeSingleUseAndRevocation(t *testing.T) {
	s := memory(t)
	code, err := s.Invite("client", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, id, err := s.Pair(code, "phone", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Pair(code, "second", ""); err == nil {
		t.Fatal("pairing code reused")
	}
	if _, err = s.Authenticate(token); err != nil {
		t.Fatal(err)
	}
	if err = s.Revoke(id.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(token); err == nil {
		t.Fatal("revoked identity authenticated")
	}
}

func TestRemoteCannotOverwriteOtherDevice(t *testing.T) {
	s := memory(t)
	original := protocol.Session{ID: "shared", DeviceID: "node-a"}
	if err := s.Put("sessions", original.ID, original); err != nil {
		t.Fatal(err)
	}
	forged := protocol.Session{ID: "shared", DeviceID: "node-b"}
	e := protocol.Event{ID: "bad", DeviceID: "node-b", SessionID: "shared", Kind: "session.updated", Payload: protocol.JSON(forged)}
	if _, err := s.Ingest(e); err == nil {
		t.Fatal("cross-device overwrite accepted")
	}
	events, _ := s.Events(0, "", 10)
	if len(events) != 0 {
		t.Fatal("invalid event was not rolled back")
	}
}

func TestEngineEventsCannotImpersonateAnotherDeviceSession(t *testing.T) {
	s := memory(t)
	if err := s.Put("sessions", "a", protocol.Session{ID: "a", DeviceID: "node-a"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Ingest(protocol.Event{ID: "forged-message", DeviceID: "node-b", SessionID: "a", Kind: "engine.item/agentMessage/delta", Payload: protocol.JSON(map[string]string{"delta": "forged"})})
	if err == nil {
		t.Fatal("foreign executor wrote into another conversation")
	}
	events, _ := s.Events(0, "a", 10)
	if len(events) != 0 {
		t.Fatal("rejected event persisted")
	}
}

func TestInteractionIdentityAndTerminalStateCannotBeReused(t *testing.T) {
	s := memory(t)
	for _, id := range []string{"a", "b"} {
		if err := s.Put("sessions", id, protocol.Session{ID: id, DeviceID: "node"}); err != nil {
			t.Fatal(err)
		}
	}
	view := protocol.Interaction{ID: "request", SessionID: "a", State: "pending", Command: "echo reviewed"}
	put := func(id string, request protocol.Interaction) error {
		_, err := s.Ingest(protocol.Event{ID: id, DeviceID: "node", SessionID: request.SessionID, Kind: "interaction.updated", Payload: protocol.JSON(request)})
		return err
	}
	if err := put("pending", view); err != nil {
		t.Fatal(err)
	}
	changed := view
	changed.SessionID = "b"
	if put("other-session", changed) == nil {
		t.Fatal("interaction ID changed its conversation")
	}
	changed = view
	changed.Command = "different command"
	if put("different-command", changed) == nil {
		t.Fatal("reviewed command was replaced")
	}
	view.State = "expired"
	if err := put("expired", view); err != nil {
		t.Fatal(err)
	}
	view.State = "pending"
	if put("reopened", view) == nil {
		t.Fatal("expired approval reopened")
	}
	var stored protocol.Interaction
	if err := s.Get("interactions", "request", &stored); err != nil {
		t.Fatal(err)
	}
	if stored.State != "expired" || stored.Command != "echo reviewed" {
		t.Fatal("rejected update changed the projection")
	}
}

func TestPolicySnapshotCannotBeRewritten(t *testing.T) {
	s := memory(t)
	if err := s.Put("sessions", "s", protocol.Session{ID: "s", DeviceID: "node"}); err != nil {
		t.Fatal(err)
	}
	snapshot := protocol.PolicySnapshot{ID: "s:version", SessionID: "s", Version: "version", DeveloperInstructions: "reviewed original", CreatedAt: "first"}
	put := func(id string, value protocol.PolicySnapshot) error {
		_, err := s.Ingest(protocol.Event{ID: id, SessionID: "s", DeviceID: "node", Kind: "policy.snapshot", Payload: protocol.JSON(value)})
		return err
	}
	if err := put("one", snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.DeveloperInstructions = "rewritten"
	if put("two", snapshot) == nil {
		t.Fatal("historical instructions were rewritten")
	}
	var actual protocol.PolicySnapshot
	if err := s.Get("policies", "s:version", &actual); err != nil {
		t.Fatal(err)
	}
	if actual.DeveloperInstructions != "reviewed original" {
		t.Fatal("rejected snapshot changed history")
	}
}

func TestSessionProjectCannotBeRelabelled(t *testing.T) {
	s := memory(t)
	original := protocol.Session{ID: "s", DeviceID: "node", ProjectID: "private"}
	if err := s.Put("sessions", "s", original); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.ProjectID = "public"
	if _, err := s.Ingest(protocol.Event{ID: "move", SessionID: "s", DeviceID: "node", Kind: "session.updated", Payload: protocol.JSON(changed)}); err == nil {
		t.Fatal("project relabelling exposed prior conversation history")
	}
}

func TestReservedExecutorDeviceDoesNotConsumeInvitation(t *testing.T) {
	s := memory(t)
	code, err := s.Invite("executor", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Pair(code, "invalid", "hub", "hub"); err == nil {
		t.Fatal("executor claimed the Hub identity")
	}
	if _, _, err = s.Pair(code, "valid", "other", "hub"); err != nil {
		t.Fatal("rejected pairing consumed the invitation")
	}
}

func TestUserInputReplyLedgerKeepsOnlyItsFingerprint(t *testing.T) {
	s := memory(t)
	command := protocol.Command{ID: "reply", Kind: "respond", SourceID: "phone", DeviceID: "node", SessionID: "s", Response: protocol.JSON(map[string]any{"action": "accept", "answers": map[string][]string{"password": {"SECRET_ANSWER"}}})}
	if _, owner, err := s.Claim(command); err != nil || !owner {
		t.Fatal(err)
	}
	var request []byte
	if err := s.db.QueryRow("SELECT request FROM commands WHERE id='reply'").Scan(&request); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(request), "SECRET_ANSWER") || !strings.Contains(string(request), "mewdexResponseSHA256") {
		t.Fatalf("reply was retained: %s", request)
	}
	if _, owner, err := s.Claim(command); err != nil || owner {
		t.Fatal("same reply fingerprint was not idempotent")
	}
	command.Response = protocol.JSON(map[string]any{"action": "accept", "answers": map[string][]string{"password": {"DIFFERENT_ANSWER"}}})
	if _, _, err := s.Claim(command); !errors.Is(err, ErrConflict) {
		t.Fatal("another answer reused the same command id")
	}
}
