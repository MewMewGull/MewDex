package store

import (
	"strings"
	"testing"

	"mewdex/internal/protocol"
)

func TestPendingAccountReadsKeepReceiptsWithoutBlockingActions(t *testing.T) {
	s := memory(t)
	for _, item := range []struct{ id, kind, state string }{
		{"read-accepted", "account.read", "accepted"},
		{"read-unknown", "account.read", "unknown"},
		{"models-accepted", "model.list", "accepted"},
		{"models-unknown", "model.list", "unknown"},
		{"login", "account.login", "unknown"},
		{"cancel", "account.cancel", "accepted"},
		{"send", "start", "unknown"},
	} {
		if _, _, err := s.Claim(protocol.Command{ID: item.id, DeviceID: "node", SourceID: "input", Kind: item.kind}); err != nil {
			t.Fatal(err)
		}
		if item.state != "accepted" {
			if err := s.Finish(protocol.CommandResult{CommandID: item.id, State: item.state}); err != nil {
				t.Fatal(err)
			}
		}
	}
	after := int64(0)
	for index, expected := range []string{"login", "cancel", "send"} {
		page, err := s.PendingCommands("input", "node", after, 1)
		if err != nil || len(page.Commands) != 1 || page.Commands[0].ID != expected || page.HasMore != (index < 2) {
			t.Fatalf("action page %d: %+v %v", index, page, err)
		}
		after = page.After
	}
	read, err := s.Result("read-unknown")
	if err != nil || read.State != "unknown" {
		t.Fatalf("read receipt was changed: %+v %v", read, err)
	}
	models, err := s.Result("models-unknown")
	if err != nil || models.State != "unknown" {
		t.Fatalf("model catalog receipt was changed: %+v %v", models, err)
	}
}

func TestPendingReferencesRecoverOwnCommandsWithoutPrivateBodies(t *testing.T) {
	s := memory(t)
	for _, id := range []string{"first", "second", "third", "done", "other-input", "other-device"} {
		command := protocol.Command{ID: id, DeviceID: "node", SourceID: "phone", Kind: "respond", CreatedAt: "created", Response: protocol.JSON(map[string]string{"answer": "secret-form-answer"}), Input: []protocol.Input{{Type: "text", Text: "private-input"}}, Policy: &protocol.Policy{Instructions: "private-policy"}}
		if id == "other-input" {
			command.SourceID = "another-phone"
		}
		if id == "other-device" {
			command.DeviceID = "another-node"
		}
		if _, fresh, err := s.Claim(command); err != nil || !fresh {
			t.Fatalf("claim: %v %v", fresh, err)
		}
		if id == "done" || id == "second" {
			state := "completed"
			if id == "second" {
				state = "unknown"
			}
			if err := s.Finish(protocol.CommandResult{CommandID: id, State: state}); err != nil {
				t.Fatal(err)
			}
		}
	}
	page, err := s.PendingCommands("phone", "node", 0, 2)
	if err != nil || len(page.Commands) != 2 || page.Commands[0].ID != "first" || page.Commands[1].ID != "second" || !page.HasMore {
		t.Fatalf("first page: %+v %v", page, err)
	}
	if page.Commands[1].State != "unknown" {
		t.Fatal("unknown result was lost")
	}
	serialized := string(protocol.JSON(page))
	for _, private := range []string{"secret-form-answer", "private-input", "private-policy", "response", "fingerprint"} {
		if strings.Contains(serialized, private) {
			t.Fatalf("private ledger field leaked: %s", private)
		}
	}
	// Finishing an earlier receipt cannot shift the cursor or duplicate the next page.
	if err = s.Finish(protocol.CommandResult{CommandID: "first", State: "completed"}); err != nil {
		t.Fatal(err)
	}
	next, err := s.PendingCommands("phone", "node", page.After, 2)
	if err != nil || len(next.Commands) != 1 || next.Commands[0].ID != "third" || next.HasMore {
		t.Fatalf("next page: %+v %v", next, err)
	}
}
