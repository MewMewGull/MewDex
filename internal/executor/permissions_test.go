package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func TestTurnGrantUsesReviewedPathsAndIgnoresReplyAdditions(t *testing.T) {
	x, _, fake := setup(t)
	s := createAndStart(t, x)
	material := t.TempDir()
	material, err := filepath.EvalSymlinks(material)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"threadId": s.EngineID, "turnId": s.TurnID, "cwd": x.config.Projects[0].Path, "permissions": map[string]any{"fileSystem": map[string]any{"read": []string{material}}}}
	x.receiveRequest(engine.Notification{Method: "item/permissions/requestApproval", RequestID: json.RawMessage(`71`), Params: protocol.JSON(params)})
	pending := x.pendingInteractions(s.ID)
	if len(pending) != 1 || !pending[0].CanAccept || pending[0].Permissions == nil || len(pending[0].Permissions.ReadPaths) != 1 {
		t.Fatalf("bounded grant was not offered: %+v", pending)
	}
	reply := protocol.InteractionReply{Action: "accept", Values: map[string]json.RawMessage{"network": json.RawMessage(`true`), "fileSystem": json.RawMessage(`{"write":["/"]}`)}}
	result := x.Handle(context.Background(), protocol.Command{ID: "grant", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: pending[0].ID, Response: protocol.JSON(reply)})
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.responses) != 1 {
		t.Fatal("missing grant response")
	}
	var response struct {
		Scope       string `json:"scope"`
		Permissions struct {
			FileSystem struct{ Read, Write []string } `json:"fileSystem"`
			Network    any                            `json:"network"`
		} `json:"permissions"`
	}
	if json.Unmarshal(fake.responses[0], &response) != nil || response.Scope != "turn" || response.Permissions.Network != nil || len(response.Permissions.FileSystem.Write) != 0 || len(response.Permissions.FileSystem.Read) != 1 || response.Permissions.FileSystem.Read[0] != material {
		t.Fatalf("reply exceeded reviewed grant: %s", fake.responses[0])
	}
}

func TestTurnGrantRejectsPrivateAndMisboundRequests(t *testing.T) {
	for _, mutation := range []string{"private", "parent", "old-turn", "missing-turn", "cwd", "environment"} {
		t.Run(mutation, func(t *testing.T) {
			x, _, fake := setup(t)
			s := createAndStart(t, x)
			params := map[string]any{"threadId": s.EngineID, "turnId": s.TurnID, "cwd": x.config.Projects[0].Path, "permissions": map[string]any{"network": map[string]bool{"enabled": true}}}
			switch mutation {
			case "private", "parent":
				path := x.config.DataDir
				if mutation == "parent" {
					path = filepath.Dir(path)
				}
				params["permissions"] = map[string]any{"fileSystem": map[string]any{"read": []string{path}}}
			case "old-turn":
				params["turnId"] = "other-turn"
			case "missing-turn":
				delete(params, "turnId")
			case "cwd":
				params["cwd"] = t.TempDir()
			case "environment":
				params["environmentId"] = "other-environment"
			}
			x.receiveRequest(engine.Notification{Method: "item/permissions/requestApproval", RequestID: json.RawMessage(`72`), Params: protocol.JSON(params)})
			pending := x.pendingInteractions(s.ID)
			if len(pending) != 1 || pending[0].CanAccept {
				t.Fatalf("unsafe grant offered: %+v", pending)
			}
			result := x.Handle(context.Background(), protocol.Command{ID: "unsafe", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: pending[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
			if result.State != "failed" {
				t.Fatal("unsafe grant accepted")
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if len(fake.responses) != 0 {
				t.Fatal("unsafe grant reached engine")
			}
		})
	}
}

func TestPermissionPathAliasIsRecheckedBeforeResponding(t *testing.T) {
	x, _, fake := setup(t)
	s := createAndStart(t, x)
	root := t.TempDir()
	a, b, alias := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "alias")
	for _, path := range []string{a, b} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(a, alias); err != nil {
		t.Skip("symlink creation is unavailable", err)
	}
	params := map[string]any{"threadId": s.EngineID, "turnId": s.TurnID, "cwd": x.config.Projects[0].Path, "permissions": map[string]any{"fileSystem": map[string]any{"read": []string{alias}}}}
	x.receiveRequest(engine.Notification{Method: "item/permissions/requestApproval", RequestID: json.RawMessage(`73`), Params: protocol.JSON(params)})
	pending := x.pendingInteractions(s.ID)
	if len(pending) != 1 || !pending[0].CanAccept {
		t.Fatal("literal alias was not resolved")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, alias); err != nil {
		t.Fatal(err)
	}
	result := x.Handle(context.Background(), protocol.Command{ID: "changed-alias", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: pending[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
	if result.State != "failed" {
		t.Fatal("changed permission target was accepted")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.responses) != 0 {
		t.Fatal("changed target reached the engine")
	}
}

func TestCommandApprovalOffersOnlyOneBoundedDecision(t *testing.T) {
	x, _, fake := setup(t)
	s := createAndStart(t, x)
	params := map[string]any{"threadId": s.EngineID, "turnId": s.TurnID, "cwd": x.config.Projects[0].Path, "command": "synthetic command", "environmentId": "local", "availableDecisions": []string{"accept", "acceptForSession", "decline"}}
	x.receiveRequest(engine.Notification{Method: "item/commandExecution/requestApproval", RequestID: json.RawMessage(`74`), Params: protocol.JSON(params)})
	pending := x.pendingInteractions(s.ID)
	if len(pending) != 1 || !pending[0].CanAccept {
		t.Fatalf("bounded command not offered: %+v", pending)
	}
	for index, action := range []string{"acceptForSession", "accept"} {
		result := x.Handle(context.Background(), protocol.Command{ID: action, SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: pending[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: action})})
		if (result.State == "completed") != (index == 1) {
			t.Fatal(result.State, result.Error)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.responses) != 1 || string(fake.responses[0]) != `{"decision":"accept"}` {
		t.Fatalf("unexpected decisions: %s", fake.responses)
	}
}

func TestFileApprovalRequiresTheSameReviewedPatch(t *testing.T) {
	x, _, fake := setup(t)
	s := createAndStart(t, x)
	patch := "--- /dev/null\n+++ example.txt\n+fixture\n"
	change := func(content string) {
		x.consumeOne(engine.Notification{Method: "item/started", Params: protocol.JSON(map[string]any{"threadId": s.EngineID, "turnId": s.TurnID, "item": map[string]any{"type": "fileChange", "id": "call", "status": "inProgress", "changes": []any{map[string]any{"path": "example.txt", "kind": map[string]string{"type": "add"}, "diff": content}}}})})
	}
	change("fixture\n")
	params := map[string]any{"threadId": s.EngineID, "turnId": s.TurnID, "itemId": "call"}
	x.receiveRequest(engine.Notification{Method: "item/fileChange/requestApproval", RequestID: json.RawMessage(`75`), Params: protocol.JSON(params)})
	pending := x.pendingInteractions(s.ID)
	if len(pending) != 1 || !pending[0].CanAccept || pending[0].FilePatch != patch {
		t.Fatalf("patch not offered: %+v", pending)
	}
	change("changed\n")
	result := x.Handle(context.Background(), protocol.Command{ID: "changed-patch", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: pending[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
	if result.State != "failed" {
		t.Fatal("changed patch accepted")
	}
	change("fixture\n")
	result = x.Handle(context.Background(), protocol.Command{ID: "same-patch", SourceID: "phone", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: pending[0].ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.responses) != 1 {
		t.Fatal("changed patch was submitted")
	}
}
