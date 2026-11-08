package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func selectableManager(t *testing.T) (*Manager, *testBackend, channel) {
	t.Helper()
	m, backend, c := testManager(t)
	if err := backend.store.RegisterDevice(protocol.Device{ID: c.DeviceID, Roles: []string{"executor"}, Projects: []protocol.Project{{ID: "project", Name: "Alpha"}, {ID: "other", Name: "Beta"}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Update("qq", protocol.MessagingUpdate{Enabled: true, DeviceID: c.DeviceID, AppID: c.AppID, AllowedUsers: c.AllowedUsers, AllowedChats: c.AllowedChats}); err != nil {
		t.Fatal(err)
	}
	c, _ = m.config("qq")
	return m, backend, c
}

func chatMessage(t *testing.T, m *Manager, c channel, e Envelope, text string) (binding, string) {
	t.Helper()
	e.Text = text
	if err := m.receive(c, e); err != nil {
		t.Fatal(err)
	}
	drainInbox(t, m, context.Background())
	var b binding
	if err := m.store.Get("messaging_bindings", c.key(e), &b); err != nil {
		t.Fatal(err)
	}
	outputs, err := store.List[delivery](m.store, "messaging_outbox")
	if err != nil {
		t.Fatal(err)
	}
	var reply string
	for _, out := range outputs {
		if out.Envelope.ID == e.ID {
			reply += out.Text
		}
	}
	return b, reply
}

func TestSelectableChannelRequiresExplicitProjectAndPersistsChoice(t *testing.T) {
	m, backend, c := selectableManager(t)
	b, reply := chatMessage(t, m, c, inbound("initial", "alice"), "修改文件")
	if len(backend.commands) != 0 || b.ProjectID != "" || !strings.Contains(reply, "1. Alpha [project]") || !strings.Contains(reply, "/project 1") {
		t.Fatal("an unselected chat ran work or omitted project instructions", b, reply)
	}
	b, _ = chatMessage(t, m, c, inbound("choose", "alice"), "/project 2")
	if b.ProjectID != "other" || b.SessionID != "" || len(backend.commands) != 0 {
		t.Fatal("selection created a session or selected the wrong project", b)
	}
	restarted, err := New(m.store, backend.submit)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = chatMessage(t, restarted, c, inbound("task", "alice"), "修改文件")
	if len(backend.commands) != 2 || backend.commands[0].ProjectID != "other" || backend.commands[1].ProjectID != "other" || b.SessionID == "" {
		t.Fatal("restart lost the selected target", backend.commands)
	}
	b, _ = chatMessage(t, restarted, c, inbound("same", "alice"), "/project Beta")
	if b.SessionID != backend.commands[1].SessionID {
		t.Fatal("selecting the same project discarded its active session")
	}
}

func TestProjectSelectionIsIndependentForUsersChatsAndThreads(t *testing.T) {
	m, backend, c := selectableManager(t)
	envelopes := []Envelope{
		{ID: "alice-one", UserID: "alice", ChatID: "group", Scene: "group", Group: true, Mentioned: true, ThreadID: "one"},
		{ID: "bob-one", UserID: "bob", ChatID: "group", Scene: "group", Group: true, Mentioned: true, ThreadID: "one"},
		{ID: "alice-two", UserID: "alice", ChatID: "group", Scene: "group", Group: true, Mentioned: true, ThreadID: "two"},
		inbound("alice-direct", "alice"),
	}
	for index, e := range envelopes {
		project := "project"
		if index == 1 {
			project = "other"
		}
		chatMessage(t, m, c, e, "/project "+project)
		e.ID += "-task"
		chatMessage(t, m, c, e, "开始任务")
	}
	bindings, err := store.List[binding](m.store, "messaging_bindings")
	if err != nil || len(bindings) != 4 || len(backend.commands) != 8 {
		t.Fatal("independent selections collided", bindings, backend.commands, err)
	}
	for index, command := range backend.commands {
		expected := "project"
		if index/2 == 1 {
			expected = "other"
		}
		if command.ProjectID != expected {
			t.Fatal("another user's selection changed the command target", command)
		}
	}
}

func TestProjectNumbersUseDisplayedCatalogueAndRecheckAccess(t *testing.T) {
	m, backend, c := selectableManager(t)
	chatMessage(t, m, c, inbound("list", "alice"), "/projects")
	if err := backend.store.RegisterDevice(protocol.Device{ID: c.DeviceID, Roles: []string{"executor"}, Projects: []protocol.Project{{ID: "first", Name: "Aardvark"}, {ID: "project", Name: "Alpha"}, {ID: "other", Name: "Beta"}}}); err != nil {
		t.Fatal(err)
	}
	restarted, _ := New(m.store, backend.submit)
	b, _ := chatMessage(t, restarted, c, inbound("number", "alice"), "/project 2")
	if b.ProjectID != "other" {
		t.Fatal("catalogue changes or restart silently renumbered the user's choice", b)
	}
	access := protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: c.DeviceID, ProjectIDs: []string{"project"}}}}
	if err := backend.store.UpdateAccess(c.IdentityID, access); err != nil {
		t.Fatal(err)
	}
	_, reply := chatMessage(t, restarted, c, inbound("revoked-task", "alice"), "不得执行")
	if len(backend.commands) != 0 || !strings.Contains(reply, "访问权限已撤销") {
		t.Fatal("a revoked selection executed work", reply, backend.commands)
	}
	b, reply = chatMessage(t, restarted, c, inbound("revoked-choice", "alice"), "/project 2")
	if b.ProjectID != "other" || strings.Contains(reply, "Beta") || !strings.Contains(reply, "Alpha") {
		t.Fatal("the project list exposed or selected a disallowed target", b, reply)
	}
	outputs, _ := store.List[delivery](m.store, "messaging_outbox")
	for _, out := range outputs {
		if out.Envelope.ID == "revoked-choice" && restarted.authorizeDelivery(c, out) != nil {
			t.Fatal("project selection guidance could not be delivered after revocation")
		}
	}
	b, _ = chatMessage(t, restarted, c, inbound("allowed-choice", "alice"), "/project Alpha")
	if b.ProjectID != "project" {
		t.Fatal("the user could not recover to an allowed project", b)
	}
}

func TestProjectSelectionRejectsUnknownAmbiguousAndRemovedTargets(t *testing.T) {
	m, backend, c := selectableManager(t)
	if err := backend.store.RegisterDevice(protocol.Device{ID: c.DeviceID, Roles: []string{"executor"}, Projects: []protocol.Project{{ID: "project", Name: "Same Name"}, {ID: "other", Name: "Same Name"}}}); err != nil {
		t.Fatal(err)
	}
	for index, command := range []string{"/project missing", "/project Same Name", "/project 0", "/project 99", "/project -1"} {
		b, reply := chatMessage(t, m, c, inbound(fmt.Sprint(index), "alice"), command)
		if b.ProjectID != "" || len(backend.commands) != 0 || !strings.Contains(reply, "可选项目") {
			t.Fatal("an invalid choice selected a project", command, b, reply)
		}
	}
	b, _ := chatMessage(t, m, c, inbound("valid", "alice"), "/project other")
	if b.ProjectID != "other" {
		t.Fatal("a duplicate project name prevented selection by ID")
	}
	if err := backend.store.RegisterDevice(protocol.Device{ID: c.DeviceID, Roles: []string{"executor"}, Projects: []protocol.Project{{ID: "project", Name: "Remaining"}}}); err != nil {
		t.Fatal(err)
	}
	_, reply := chatMessage(t, m, c, inbound("removed", "alice"), "不得执行")
	if len(backend.commands) != 0 || !strings.Contains(reply, "当前项目已不可用") {
		t.Fatal("a removed selection ran work", reply)
	}
}

func TestProjectSwitchRequiresSettledExecutionAndQueue(t *testing.T) {
	m, backend, c := selectableManager(t)
	chatMessage(t, m, c, inbound("choose", "alice"), "/project Alpha")
	b, _ := chatMessage(t, m, c, inbound("task", "alice"), "任务")
	sessionID := b.SessionID
	b, reply := chatMessage(t, m, c, inbound("busy-switch", "alice"), "/project Beta")
	if b.ProjectID != "project" || b.SessionID != sessionID || !strings.Contains(reply, "/stop") {
		t.Fatal("a project switch abandoned active execution", b, reply)
	}
	var session protocol.Session
	if err := backend.store.Get("sessions", sessionID, &session); err != nil {
		t.Fatal(err)
	}
	session.State = "idle"
	if err := backend.store.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	if err := backend.store.Put("queue_items", "waiting", protocol.QueueItem{ID: "waiting", SessionID: sessionID, State: "queued"}); err != nil {
		t.Fatal(err)
	}
	b, reply = chatMessage(t, m, c, inbound("queued-switch", "alice"), "/project Beta")
	if b.ProjectID != "project" || !strings.Contains(reply, "队列消息") {
		t.Fatal("a project switch abandoned queued messages", b, reply)
	}
	if err := backend.store.Delete("queue_items", "waiting"); err != nil {
		t.Fatal(err)
	}
	// The old final reply may be beyond the first event page when switching.
	for index := range 205 {
		kind, payload := "engine.progress", json.RawMessage(`{}`)
		if index == 204 {
			kind, payload = "engine.item/completed", protocol.JSON(map[string]any{"item": map[string]string{"type": "agentMessage", "phase": "final_answer", "text": "原项目回复"}})
		}
		if _, err := backend.store.Append(protocol.Event{ID: fmt.Sprint(index), DeviceID: c.DeviceID, SessionID: sessionID, Kind: kind, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	b, _ = chatMessage(t, m, c, inbound("settled-switch", "alice"), "/project Beta")
	if b.ProjectID != "other" || b.SessionID != "" || b.CreateID != "" || b.Cursor != 0 {
		t.Fatal("a settled switch retained the old session", b)
	}
	outputs, _ := store.List[delivery](m.store, "messaging_outbox")
	found := false
	for _, out := range outputs {
		found = found || out.Text == "原项目回复" && out.ProjectID == "project"
	}
	if !found {
		t.Fatal("switching discarded the old session's final reply")
	}
	b, _ = chatMessage(t, m, c, inbound("next", "alice"), "新任务")
	if len(backend.commands) != 4 || backend.commands[2].ProjectID != "other" || b.SessionID == sessionID {
		t.Fatal("the next task reused the previous project session", backend.commands)
	}
}

func TestClearingLegacyProjectPreservesConversationAndPendingReceipt(t *testing.T) {
	m, backend, legacy := testManager(t)
	e := inbound("original", "alice")
	b, _ := chatMessage(t, m, legacy, e, "任务")
	ctx, cancel := context.WithCancel(context.Background())
	backend.after = func(command protocol.Command) {
		if command.Kind == "enqueue" {
			cancel()
		}
	}
	if err := m.receive(legacy, inbound("pending", "alice")); err != nil {
		t.Fatal(err)
	}
	drainInbox(t, m, ctx)
	if err := m.Update("qq", protocol.MessagingUpdate{Enabled: true, DeviceID: legacy.DeviceID, AppID: legacy.AppID, AllowedUsers: legacy.AllowedUsers}); err != nil {
		t.Fatal(err)
	}
	c, _ := m.config("qq")
	if c.ProjectID != "" || c.key(e) != legacy.key(e) {
		t.Fatal("clearing a fixed project changed the conversation key")
	}
	restarted, _ := New(m.store, backend.submit)
	drainInbox(t, restarted, context.Background())
	pending, _ := m.store.PendingObjects("messaging_inbox", 100)
	current, _ := chatMessage(t, restarted, c, inbound("status", "alice"), "/status")
	if len(pending) != 0 || len(backend.commands) != 3 || current.SessionID != b.SessionID || current.ProjectID != legacy.ProjectID {
		t.Fatal("legacy migration lost or replayed the original session", current, backend.commands)
	}
}

func TestSelectableChannelCanEnableBeforeProjectsExistAndScopesReplies(t *testing.T) {
	m, backend, c := selectableManager(t)
	if err := backend.store.RegisterDevice(protocol.Device{ID: c.DeviceID, Roles: []string{"executor"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Update("qq", protocol.MessagingUpdate{Enabled: true, DeviceID: c.DeviceID, AppID: c.AppID, AllowedUsers: c.AllowedUsers}); err != nil {
		t.Fatal("enabling an unbound channel required a project", err)
	}
	b, reply := chatMessage(t, m, c, inbound("empty", "alice"), "/projects")
	if b.ProjectID != "" || !strings.Contains(reply, "暂无可用项目") {
		t.Fatal("the empty catalogue was not handled", b, reply)
	}
	identity, err := backend.store.Identity(c.IdentityID)
	if err != nil || identity.Role != "client" || identity.Access.AllDevices || identity.Access.Gateway || identity.Access.Allows("different-device", "project") || !identity.Access.Allows(c.DeviceID, "future-project") {
		t.Fatal("dynamic project access escaped the configured device", identity, err)
	}
	b.ProjectID = "project"
	out := replyObjects(b, "project-reply", "私有回复")[0].Value.(delivery)
	if err := backend.store.UpdateAccess(c.IdentityID, protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: c.DeviceID, ProjectIDs: []string{"other"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.authorizeDelivery(c, out); err == nil {
		t.Fatal("a queued reply bypassed project revocation")
	}
	out.IdentityID, out.DeviceID, out.ProjectID = "", "", ""
	if err := m.authorizeDelivery(c, out); err == nil {
		t.Fatal("a legacy reply bypassed its former project's revocation")
	}
}
