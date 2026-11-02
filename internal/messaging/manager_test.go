package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type testBackend struct {
	store    *store.Store
	commands []protocol.Command
	after    func(protocol.Command)
}

func (backend *testBackend) submit(_ context.Context, identity string, command protocol.Command) (protocol.CommandResult, error) {
	command.SourceID, command.CreatedAt = identity, protocol.Now()
	result, owner, err := backend.store.Claim(command)
	if err != nil || !owner {
		return result, err
	}
	backend.commands = append(backend.commands, command)
	var session protocol.Session
	if command.Kind == "create" {
		session = protocol.Session{ID: "s_" + command.ID, DeviceID: command.DeviceID, ProjectID: command.ProjectID, State: "idle", PolicyVersion: "policy"}
	} else {
		_ = backend.store.Get("sessions", command.SessionID, &session)
		if command.Kind == "start" {
			session.State, session.TurnID = "running", "turn"
		}
	}
	err = backend.store.Put("sessions", session.ID, session)
	if err != nil {
		return result, err
	}
	result.State, result.Session = "completed", &session
	if err = backend.store.Finish(result); err != nil {
		return result, err
	}
	if backend.after != nil {
		backend.after(command)
	}
	return result, nil
}

func testManager(t *testing.T) (*Manager, *testBackend, channel) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err = s.RegisterDevice(protocol.Device{ID: "device", Roles: []string{"executor"}, Projects: []protocol.Project{{ID: "project"}, {ID: "other"}}, Online: true}); err != nil {
		t.Fatal(err)
	}
	backend := &testBackend{store: s}
	m, err := New(s, backend.submit)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Update("qq", protocol.MessagingUpdate{Enabled: true, DeviceID: "device", ProjectID: "project", AppID: "app", AllowedUsers: []string{"alice", "bob"}, AllowedChats: []string{"group"}}); err != nil {
		t.Fatal(err)
	}
	c, _ := m.config("qq")
	return m, backend, c
}

func inbound(id, user string) Envelope {
	return Envelope{ID: id, Platform: "qq", Account: "app", Scene: "c2c", ChatID: user, UserID: user, Text: "修改项目中的文件", ReplyToken: id}
}

func drainInbox(t *testing.T, m *Manager, ctx context.Context) {
	t.Helper()
	raw, err := m.store.PendingObjects("messaging_inbox", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range raw {
		var item inbox
		if err = json.Unmarshal(value, &item); err != nil {
			t.Fatal(err)
		}
		m.process(ctx, item)
	}
}

func TestInboundDedupSurvivesManagerRestartAndBusyInputQueues(t *testing.T) {
	m, backend, c := testManager(t)
	e := inbound("first", "alice")
	if err := m.receive(c, e); err != nil {
		t.Fatal(err)
	}
	drainInbox(t, m, context.Background())
	if len(backend.commands) != 2 || backend.commands[0].Kind != "create" || backend.commands[1].Kind != "start" {
		t.Fatalf("unexpected initial commands: %#v", backend.commands)
	}
	restarted, err := New(m.store, backend.submit)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = restarted.receive(c, e); err != nil {
			t.Fatal(err)
		}
	}
	drainInbox(t, restarted, context.Background())
	if len(backend.commands) != 2 {
		t.Fatal("platform redelivery started duplicate work")
	}
	if err = restarted.receive(c, inbound("second", "alice")); err != nil {
		t.Fatal(err)
	}
	drainInbox(t, restarted, context.Background())
	if len(backend.commands) != 3 || backend.commands[2].Kind != "enqueue" || backend.commands[2].SessionID != backend.commands[1].SessionID {
		t.Fatalf("busy input did not enter the original session queue: %#v", backend.commands)
	}
}

func TestCrashAfterSubmissionReusesExactReceipt(t *testing.T) {
	m, backend, c := testManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	backend.after = func(command protocol.Command) {
		if command.Kind == "start" {
			cancel()
		}
	}
	if err := m.receive(c, inbound("crash", "alice")); err != nil {
		t.Fatal(err)
	}
	drainInbox(t, m, ctx)
	pending, _ := m.store.PendingObjects("messaging_inbox", 100)
	if len(pending) != 1 || len(backend.commands) != 2 {
		t.Fatal("interrupted input was not retained")
	}
	restarted, _ := New(m.store, backend.submit)
	drainInbox(t, restarted, context.Background())
	pending, _ = m.store.PendingObjects("messaging_inbox", 100)
	if len(pending) != 0 || len(backend.commands) != 2 {
		t.Fatal("restart repeated an already dispatched command")
	}
}

func TestGroupRequiresAllowedUserChatAndMentionAndIsolatesUsers(t *testing.T) {
	m, backend, c := testManager(t)
	for index, e := range []Envelope{
		{UserID: "alice", ChatID: "group", Group: true, Mentioned: false},
		{UserID: "alice", ChatID: "unknown-group", Group: true, Mentioned: true},
		{UserID: "mallory", ChatID: "group", Group: true, Mentioned: true},
		{UserID: "alice", ChatID: "group", Group: true, Mentioned: true},
		{UserID: "bob", ChatID: "group", Group: true, Mentioned: true},
	} {
		e.ID, e.Scene, e.Text = string(rune('a'+index)), "group", "hello"
		if err := m.receive(c, e); err != nil {
			t.Fatal(err)
		}
	}
	drainInbox(t, m, context.Background())
	bindings, _ := store.List[binding](m.store, "messaging_bindings")
	if len(bindings) != 2 || len(backend.commands) != 4 || bindings[0].SessionID == bindings[1].SessionID {
		t.Fatalf("group users shared a session or unauthorized input ran: %#v", bindings)
	}
	otherPlatform := c
	otherPlatform.Platform = "feishu"
	if c.key(bindings[0].Envelope) == otherPlatform.key(bindings[0].Envelope) {
		t.Fatal("platform namespaces collided")
	}
}

func TestDisabledRemovedUserAndRevokedIdentityCannotExecute(t *testing.T) {
	for _, change := range []string{"disabled", "user", "identity", "project"} {
		t.Run(change, func(t *testing.T) {
			m, backend, c := testManager(t)
			e := inbound("pending", "alice")
			if err := m.receive(c, e); err != nil {
				t.Fatal(err)
			}
			update := protocol.MessagingUpdate{Enabled: true, DeviceID: c.DeviceID, ProjectID: c.ProjectID, AppID: c.AppID, AllowedUsers: c.AllowedUsers, AllowedChats: c.AllowedChats}
			switch change {
			case "disabled":
				update.Enabled = false
			case "user":
				update.AllowedUsers = []string{"bob"}
			case "project":
				update.ProjectID = "other"
			case "identity":
				if err := m.store.Revoke(c.IdentityID); err != nil {
					t.Fatal(err)
				}
			}
			if change != "identity" {
				if err := m.Update("qq", update); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.authorize(c, e); err == nil {
				t.Fatal("stale authorization remained usable")
			}
			drainInbox(t, m, context.Background())
			if len(backend.commands) != 0 {
				t.Fatal("revoked pending input executed")
			}
		})
	}
}

func TestReplyCheckpointPreventsDuplicateOutputsAfterRestart(t *testing.T) {
	m, _, c := testManager(t)
	e := inbound("event", "alice")
	_ = m.receive(c, e)
	drainInbox(t, m, context.Background())
	bindings, _ := store.List[binding](m.store, "messaging_bindings")
	b := bindings[0]
	for _, phase := range []string{"commentary", "final_answer"} {
		_, err := m.store.Append(protocol.Event{ID: phase, DeviceID: b.DeviceID, SessionID: b.SessionID, Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": map[string]string{"type": "agentMessage", "text": "完成的回复", "phase": phase}})})
		if err != nil {
			t.Fatal(err)
		}
	}
	m.checkEvents()
	restarted, _ := New(m.store, m.submit)
	restarted.checkEvents()
	outputs, _ := store.List[delivery](m.store, "messaging_outbox")
	if len(outputs) != 1 || outputs[0].Text != "完成的回复" || outputs[0].Envelope.UserID != "alice" {
		t.Fatalf("reply was repeated, misrouted or included progress noise: %#v", outputs)
	}
}

func TestExternalApprovalStaysInSessionAndCannotGrantPermissions(t *testing.T) {
	m, backend, c := testManager(t)
	_ = m.receive(c, inbound("init", "alice"))
	drainInbox(t, m, context.Background())
	bindings, _ := store.List[binding](m.store, "messaging_bindings")
	b := bindings[0]
	for _, request := range []protocol.Interaction{
		{ID: "extra", SessionID: b.SessionID, Kind: "permissions", Permissions: &protocol.PermissionGrant{Network: true}},
		{ID: "other", SessionID: "another-session", Kind: "command"},
		{ID: "files", SessionID: b.SessionID, Kind: "files"},
		{ID: "question", SessionID: b.SessionID, Kind: "questions", Questions: []protocol.InputQuestion{{ID: "choice"}}},
		{ID: "bounded", SessionID: b.SessionID, Kind: "command"},
	} {
		request.State, request.CanAccept, request.ExecutionScopeID = "pending", true, "scope"
		_ = m.store.Put("interactions", request.ID, request)
	}
	for index, text := range []string{"/approve extra", "/approve other", "/approve files", `/answer question {"choice":["yes"]}`, "/approve bounded"} {
		e := inbound(string(rune('0'+index)), "alice")
		e.Text = text
		_ = m.receive(c, e)
		drainInbox(t, m, context.Background())
	}
	if len(backend.commands) != 4 || backend.commands[2].RequestID != "question" || backend.commands[3].RequestID != "bounded" {
		t.Fatalf("approval escaped session or permission boundaries: %#v", backend.commands)
	}
	var answer protocol.InteractionReply
	_ = json.Unmarshal(backend.commands[2].Response, &answer)
	if answer.Action != "accept" || answer.Answers["choice"][0] != "yes" {
		t.Fatal("question reply did not use the executor's supported contract")
	}
}

func TestSnapshotNeverReturnsCredentials(t *testing.T) {
	m, _, c := testManager(t)
	secret := "test-secret-do-not-expose"
	if err := m.Update("qq", protocol.MessagingUpdate{DeviceID: c.DeviceID, ProjectID: c.ProjectID, AppID: c.AppID, Secret: &secret}); err != nil {
		t.Fatal(err)
	}
	data := string(protocol.JSON(m.Snapshot()))
	if strings.Contains(data, secret) || !strings.Contains(data, `"credentialConfigured":true`) {
		t.Fatal("snapshot exposed or lost credential metadata")
	}
}

type testAdapter struct {
	mu      sync.Mutex
	sends   []string
	started chan struct{}
}

func (a *testAdapter) Run(ctx context.Context, _ func(Envelope) error, status func(string, string)) error {
	status("connected", "test")
	close(a.started)
	<-ctx.Done()
	return ctx.Err()
}

func (a *testAdapter) Send(_ context.Context, _ Envelope, _ string, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sends = append(a.sends, id)
	if len(a.sends) == 1 {
		return errors.New("temporary send failure")
	}
	return nil
}

func TestOutboxRetriesOnlyItsFailedChunkAndChannelStopsCleanly(t *testing.T) {
	m, _, c := testManager(t)
	secret := "mock-credential"
	if err := m.Update("qq", protocol.MessagingUpdate{Enabled: true, DeviceID: c.DeviceID, ProjectID: c.ProjectID, AppID: c.AppID, AllowedUsers: c.AllowedUsers, Secret: &secret}); err != nil {
		t.Fatal(err)
	}
	a := &testAdapter{started: make(chan struct{})}
	m.factory = func(channel) adapter { return a }
	e := inbound("source", "alice")
	b := binding{ID: c.key(e), Envelope: e, IdentityID: c.IdentityID, DeviceID: c.DeviceID, ProjectID: c.ProjectID}
	if err := m.store.PutObjects(replyObjects(b, "long-reply", strings.Repeat("文", 2000))...); err != nil {
		t.Fatal(err)
	}
	m.Start(context.Background())
	t.Cleanup(m.Close)
	select {
	case <-a.started:
	case <-time.After(3 * time.Second):
		t.Fatal("adapter did not start")
	}
	finished := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := store.List[delivery](m.store, "messaging_outbox")
		if err == nil && len(rows) == 2 && rows[0].State == "sent" && rows[1].State == "sent" {
			finished = rows[0].Text == "" && rows[1].Text == ""
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.mu.Lock()
	sends := append([]string{}, a.sends...)
	a.mu.Unlock()
	if !finished || len(sends) != 3 || sends[0] != sends[1] || sends[1] == sends[2] {
		t.Fatalf("chunk delivery lost ordering or replayed successful chunks: %#v", sends)
	}
	if err := m.Update("qq", protocol.MessagingUpdate{AppID: c.AppID}); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot()[1].State != "disabled" {
		t.Fatal("saved credentials re-enabled a disabled channel")
	}
	m.Close()
	if err := m.Update("qq", protocol.MessagingUpdate{}); err == nil {
		t.Fatal("a stopped manager accepted a reconfiguration")
	}
}

func TestLastReplyDoesNotStartAnotherModelTurnAndQQReplyBudgetIsBounded(t *testing.T) {
	m, backend, c := testManager(t)
	e := inbound("initial", "alice")
	_ = m.receive(c, e)
	drainInbox(t, m, context.Background())
	bindings, _ := store.List[binding](m.store, "messaging_bindings")
	b := bindings[0]
	_, err := m.store.Append(protocol.Event{ID: "answer", DeviceID: b.DeviceID, SessionID: b.SessionID, Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": map[string]string{"type": "agentMessage", "phase": "final_answer", "text": "最近的回复"}})})
	if err != nil {
		t.Fatal(err)
	}
	e.ID, e.Text = "last", "/last"
	_ = m.receive(c, e)
	drainInbox(t, m, context.Background())
	outputs, _ := store.List[delivery](m.store, "messaging_outbox")
	if len(backend.commands) != 2 || len(outputs) != 1 || outputs[0].Text != "最近的回复" || outputs[0].Envelope.ID != "last" {
		t.Fatal("/last reran inference or did not use its new passive reply context")
	}
	chunks := replyObjects(b, "very-long", strings.Repeat("中文🙂", 8000))
	if len(chunks) != 4 || !strings.Contains(chunks[3].Value.(delivery).Text, "完整回复") || len(chunks[3].Value.(delivery).Text) > 3500 {
		t.Fatal("long QQ reply exceeded its passive message budget or was silently cut")
	}
}
