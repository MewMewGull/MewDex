package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mewdex/internal/engine"
	"mewdex/internal/policy"
	"mewdex/internal/protocol"
)

func TestScopeIsBoundAcrossCreateTurnResumeAndFork(t *testing.T) {
	x, state, fake := setup(t)
	s := createAndStart(t, x)
	if s.ExecutionScopeID == "" {
		t.Fatal("conversation has no bound scope")
	}
	for _, method := range []string{"thread/start", "turn/start"} {
		params := lastParameters(t, fake, method)
		if (method == "thread/start" && params["permissions"] != s.ExecutionScopeID) || (method == "turn/start" && params["permissions"] != nil) || params["sandbox"] != nil || params["sandboxPolicy"] != nil {
			t.Fatalf("%s lost its profile or reintroduced a legacy fallback", method)
		}
	}
	finishTurn(x, s)
	if err := state.Get("sessions", s.ID, &s); err != nil {
		t.Fatal(err)
	}
	x.mu.Lock()
	x.loaded[s.ID] = false
	x.mu.Unlock()
	if err := x.load(context.Background(), &s); err != nil {
		t.Fatal(err)
	}
	resume := lastParameters(t, fake, "thread/resume")
	if resume["permissions"] != s.ExecutionScopeID || resume["baseInstructions"] != nil {
		t.Fatal("resume changed the initial base before a reviewed fork")
	}
	base := "new base"
	s.Policy.BaseInstructions = &base
	setPolicy(&s, s.Policy)
	if s.BaseVersion == policy.BaseVersion(s.Policy) {
		t.Fatal("fixture did not change base policy")
	}
	if err := x.reconfigureBase(context.Background(), &s); err != nil {
		t.Fatal(err)
	}
	fork := lastParameters(t, fake, "thread/fork")
	if fork["permissions"] != s.ExecutionScopeID || fork["baseInstructions"] != base {
		t.Fatal("fork lost scope or base instructions")
	}
}

func TestMissingEngineScopeReceiptCannotStartAConversation(t *testing.T) {
	x, _, fake := setup(t)
	fake.scopeResponseDrift = true
	result := x.Handle(context.Background(), protocol.Command{ID: "unconfirmed-scope", DeviceID: "device", ProjectID: "project", SourceID: "client", Kind: "create"})
	if result.State != "unknown" || result.Session != nil {
		t.Fatalf("unconfirmed effective permissions became usable: %+v", result)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, method := range fake.methods {
		if method == "turn/start" {
			t.Fatal("model turn reached an unconfirmed profile")
		}
	}
}

func TestScopeChangeStopsTheNextTurn(t *testing.T) {
	x, state, fake := setup(t)
	s := createAndStart(t, x)
	finishTurn(x, s)
	if err := state.Get("sessions", s.ID, &s); err != nil {
		t.Fatal(err)
	}
	program, _ := os.Executable()
	extra := filepath.Join(t.TempDir(), "public-resource")
	if err := os.WriteFile(extra, []byte("changed local configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.environment = &engine.ExecutionEnvironment{RuntimeReadOnly: []string{program, extra}}
	fake.mu.Unlock()
	result := x.Handle(context.Background(), protocol.Command{ID: "changed-scope-turn", SourceID: "client", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", Input: []protocol.Input{{Type: "text", Text: "must wait for configuration review"}}})
	if result.State != "failed" || !strings.Contains(result.Error, "permissions or environment changed") {
		t.Fatalf("scope changed silently: %+v", result)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	starts := 0
	for _, method := range fake.methods {
		if method == "turn/start" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatal("changed configuration reached a second model turn")
	}
}

func TestAttachmentsReachOnlyTheirSessionView(t *testing.T) {
	x, _, fake := setup(t)
	created := x.Handle(context.Background(), protocol.Command{ID: "attachment-view", DeviceID: "device", ProjectID: "project", SourceID: "client", Kind: "create"})
	if created.State != "completed" {
		t.Fatal(created.Error)
	}
	meta, err := x.attachments.SaveTarget("read.txt", strings.NewReader("scope attachment"), "client", "device", "project")
	if err != nil {
		t.Fatal(err)
	}
	result := x.Handle(context.Background(), protocol.Command{ID: "attachment-turn", DeviceID: "device", SessionID: created.Session.ID, SourceID: "client", Kind: "start", Input: []protocol.Input{{Type: "file", AttachmentID: meta.ID}}})
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	params := lastParameters(t, fake, "turn/start")
	privatePath, _ := x.attachments.Path(meta.ID)
	view := x.views[created.Session.ID]
	if view == nil {
		t.Fatal("missing bound attachment view")
	}
	inputs := params["input"].([]any)
	text := inputs[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, strconv.Quote(filepath.Join(view.Directory(), meta.ID))) || strings.Contains(text, strconv.Quote(privatePath)) {
		t.Fatal("model input does not exclusively reference the staged attachment view")
	}
	data, err := os.ReadFile(filepath.Join(view.Directory(), meta.ID))
	if err != nil || string(data) != "scope attachment" {
		t.Fatal("session view was not materialized", err)
	}
}

func TestConversationApprovalCannotEscapeItsBoundProfile(t *testing.T) {
	for _, method := range []string{"item/commandExecution/requestApproval", "item/fileChange/requestApproval"} {
		t.Run(method, func(t *testing.T) {
			x, _, fake := setup(t)
			s := createAndStart(t, x)
			params := map[string]any{"threadId": s.EngineID, "turnId": s.TurnID, "command": "unbounded command", "permissions": map[string]any{"network": map[string]bool{"enabled": true}}}
			x.receiveRequest(engine.Notification{Method: method, RequestID: json.RawMessage(`27`), Params: protocol.JSON(params)})
			pending := x.pendingInteractions(s.ID)
			if len(pending) != 1 || pending[0].CanAccept || pending[0].ExecutionScopeID != s.ExecutionScopeID {
				t.Fatal("approval offers an unbounded accept")
			}
			accept := x.Handle(context.Background(), protocol.Command{ID: "unsafe-accept", DeviceID: s.DeviceID, SessionID: s.ID, SourceID: "client", Kind: "respond", RequestID: pending[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
			if accept.State != "failed" {
				t.Fatal("unbounded approval reached the engine")
			}
			decline := x.Handle(context.Background(), protocol.Command{ID: "safe-decline", DeviceID: s.DeviceID, SessionID: s.ID, SourceID: "client", Kind: "respond", RequestID: pending[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: "decline"})})
			if decline.State != "completed" {
				t.Fatal(decline.Error)
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if len(fake.responses) != 1 {
				t.Fatal("unsafe accept was forwarded or safe decline was lost")
			}
		})
	}
}
