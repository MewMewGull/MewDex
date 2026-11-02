package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mewdex/internal/attachments"
	"mewdex/internal/protocol"
)

func TestRestoredConversationRebindsOnlyAcceptedAttachmentPaths(t *testing.T) {
	x, state, fake := setup(t)
	created := x.Handle(context.Background(), protocol.Command{ID: "create", DeviceID: "device", ProjectID: "project", Kind: "create", SourceID: "client"})
	if created.State != "completed" {
		t.Fatal(created.Error)
	}
	s := *created.Session
	meta, err := x.attachments.SaveTarget("历史附件.txt", strings.NewReader("preserved input"), "client", s.DeviceID, s.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	unused, err := x.attachments.SaveTarget("未发送.txt", strings.NewReader("not in the conversation"), "client", s.DeviceID, s.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	first := x.Handle(context.Background(), protocol.Command{ID: "first", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", SourceID: "client", Input: []protocol.Input{{Type: "file", AttachmentID: meta.ID}}})
	if first.State != "completed" {
		t.Fatal(first.Error)
	}
	finishTurn(x, *first.Session)
	if err = state.Get("sessions", s.ID, &s); err != nil {
		t.Fatal(err)
	}
	oldPolicy := s.PolicyInstructions
	oldView := x.views[s.ID].Directory()
	newData := filepath.Join(filepath.Dir(x.config.DataDir), "restored-data")
	assets, err := attachments.New(filepath.Join(newData, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	for _, attachment := range []protocol.Attachment{meta, unused} {
		file, err := x.attachments.Open(attachment.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, importErr := assets.Import(attachment, file)
		file.Close()
		if importErr != nil {
			t.Fatal(importErr)
		}
	}
	if err = state.PrepareRestore(context.Background(), s.DeviceID, true); err != nil {
		t.Fatal(err)
	}
	s = protocol.Session{ID: s.ID}
	if err = state.Get("sessions", s.ID, &s); err != nil {
		t.Fatal(err)
	}
	s.Frozen, s.State = false, "idle"
	if err = x.save(&s); err != nil {
		t.Fatal(err)
	}
	x.config.DataDir, x.attachments = newData, assets
	x.scopeMu.Lock()
	x.views, x.inputParents, x.attachmentContexts = map[string]*attachments.View{}, map[string]*attachments.View{}, map[string]attachmentContext{}
	x.scopeMu.Unlock()
	x.mu.Lock()
	x.loaded[s.ID] = false
	x.mu.Unlock()
	result := x.Handle(context.Background(), protocol.Command{ID: "after-restore", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", SourceID: "client", Input: []protocol.Input{{Type: "text", Text: "continue with the old attachment"}}})
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	context := x.attachmentContexts[s.ID]
	if context.IndexPath == "" || filepath.Dir(context.IndexPath) == oldView {
		t.Fatal("historical input kept its old path")
	}
	raw, err := os.ReadFile(context.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Files []struct{ ID, Path string } `json:"files"`
	}
	if json.Unmarshal(raw, &index) != nil || len(index.Files) != 1 || index.Files[0].ID != meta.ID {
		t.Fatal("index included an unsent upload or lost an accepted input")
	}
	content, err := os.ReadFile(index.Files[0].Path)
	if err != nil || string(content) != "preserved input" || filepath.Dir(index.Files[0].Path) == oldView {
		t.Fatal("historical attachment was not rematerialized", err)
	}
	settings := lastParameters(t, fake, "turn/start")["collaborationMode"].(map[string]any)["settings"].(map[string]any)
	if !strings.Contains(settings["developer_instructions"].(string), strconv.Quote(context.IndexPath)) || result.Session.PolicyInstructions != oldPolicy {
		t.Fatal("runtime context replaced the saved user policy")
	}
	var snapshot turnContextSnapshot
	if err = state.Get("turn_contexts", "after-restore", &snapshot); err != nil || snapshot.State != "submitted" || snapshot.AttachmentIndex != context.IndexPath || snapshot.InstructionsSHA256 == "" {
		t.Fatal("runtime instructions have no submitted snapshot", err)
	}
}

func TestHistoricalMissingOrForeignInputsBlockNewModelTurns(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "foreign"}[foreign], func(t *testing.T) {
			x, state, fake := setup(t)
			s := createAndStart(t, x)
			finishTurn(x, s)
			id := strings.Repeat("a", 32)
			if foreign {
				meta, err := x.attachments.SaveTarget("foreign.txt", strings.NewReader("other project"), "client", s.DeviceID, "other")
				if err != nil {
					t.Fatal(err)
				}
				id = meta.ID
			}
			if _, err := state.Append(protocol.Event{ID: "prior-input", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "message.user", Payload: protocol.JSON(protocol.Command{Input: []protocol.Input{{Type: "file", AttachmentID: id}}})}); err != nil {
				t.Fatal(err)
			}
			x.scopeMu.Lock()
			delete(x.attachmentContexts, s.ID)
			x.scopeMu.Unlock()
			result := x.Handle(context.Background(), protocol.Command{ID: "must-not-start", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", SourceID: "client", Input: []protocol.Input{{Type: "text", Text: "continue"}}})
			if result.State != "failed" || !strings.Contains(result.Error, "historical attachment unavailable") {
				t.Fatalf("unavailable historical inputs reached model: %+v", result)
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
				t.Fatal("started another model turn despite missing historical input")
			}
		})
	}
}
