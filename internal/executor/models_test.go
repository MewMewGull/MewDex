package executor

import (
	"context"
	"encoding/json"
	"testing"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type catalogFixture struct {
	*fixtureEngine
}

func (f *catalogFixture) ModelRuntime() protocol.ModelRuntime {
	return protocol.ModelRuntime{Adapter: "fixture", Version: "fixture-version", Transport: "jsonrpc-stdio"}
}

func (f *catalogFixture) Call(ctx context.Context, method string, params any, out any) error {
	if err := f.fixtureEngine.Call(ctx, method, params, out); err != nil || method != "model/list" {
		return err
	}
	var request struct {
		Cursor string `json:"cursor"`
	}
	if err := json.Unmarshal(protocol.JSON(params), &request); err != nil {
		return err
	}
	model := map[string]any{"id": "picker-id", "model": "provider-model", "displayName": "Fixture model", "defaultReasoningEffort": "future-effort", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "future-effort", "description": "A catalog-provided option"}}, "inputModalities": []string{"text", "image"}, "privateProviderToken": "not-public"}
	page := map[string]any{"data": []any{model, map[string]any{"id": "hidden", "hidden": true}}, "nextCursor": "opaque-next"}
	if request.Cursor == "opaque-next" {
		page = map[string]any{"data": []any{}, "nextCursor": nil}
	}
	return json.Unmarshal(protocol.JSON(page), out)
}

func TestModelCatalogPagesAreReadOnlyScopedAndProjected(t *testing.T) {
	x, data, fake := setupWithEngine(t, func(f *fixtureEngine) engine.Engine { return &catalogFixture{f} })
	command := protocol.Command{ID: "catalog-first", SourceID: "input", DeviceID: "device", ProjectID: "project", Kind: "model.list"}
	first := x.Handle(context.Background(), command)
	if first.State != "completed" {
		t.Fatal(first.Error)
	}
	var page protocol.ModelPage
	if json.Unmarshal(first.Data, &page) != nil || len(page.Models) != 1 || page.NextCursor != "opaque-next" || page.Models[0].Model != "provider-model" || page.Models[0].SupportedReasoningEfforts[0].ReasoningEffort != "future-effort" {
		t.Fatalf("catalog projection: %s", first.Data)
	}
	var public map[string]any
	if page.Runtime == nil || page.Runtime.Adapter != "fixture" || page.Runtime.Version != "fixture-version" || len(page.Models[0].InputModalities) != 2 {
		t.Fatal("catalog omitted its initialized adapter or model input metadata")
	}
	_ = json.Unmarshal(first.Data, &public)
	if _, ok := public["models"].([]any)[0].(map[string]any)["privateProviderToken"]; ok {
		t.Fatal("provider-private field reached an input")
	}
	if replay := x.Handle(context.Background(), command); string(replay.Data) != string(first.Data) {
		t.Fatal("same read command lost its original receipt")
	}
	second := x.Handle(context.Background(), protocol.Command{ID: "catalog-next", SourceID: "input", DeviceID: "device", ProjectID: "project", Kind: "model.list", Models: &protocol.ModelListRequest{Cursor: page.NextCursor, Limit: 10}})
	page = protocol.ModelPage{}
	if second.State != "completed" || json.Unmarshal(second.Data, &page) != nil || len(page.Models) != 0 || page.NextCursor != "" {
		t.Fatalf("catalog continuation: %+v %s", second, second.Data)
	}
	if request := lastParameters(t, fake, "model/list"); request["cursor"] != "opaque-next" || request["includeHidden"] != false || request["limit"] != float64(10) {
		t.Fatalf("engine pagination: %+v", request)
	}
	invalid := x.Handle(context.Background(), protocol.Command{ID: "catalog-wrong-project", DeviceID: "device", ProjectID: "missing", Kind: "model.list"})
	if invalid.State != "failed" {
		t.Fatal("unconfigured project queried an engine")
	}
	sessions, err := store.List[protocol.Session](data, "sessions")
	if err != nil || len(sessions) != 0 {
		t.Fatal("catalog reading created a conversation")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.methods) != 2 || fake.methods[0] != "model/list" || fake.methods[1] != "model/list" {
		t.Fatalf("catalog reading invoked non-read operations: %+v", fake.methods)
	}
}

func TestReasoningPolicyVersionsReachNextTurnAndClearOverride(t *testing.T) {
	x, data, fake := setup(t)
	s := createAndStart(t, x)
	if s.ModelProvider != "fixture-provider" {
		t.Fatal("thread provider was not captured in the conversation")
	}
	finishTurn(x, s)
	p := s.Policy
	p.ReasoningEffort = "future-effort"
	saved := x.Handle(context.Background(), protocol.Command{ID: "effort-save", SourceID: "input", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "policy.save", Policy: &p, ExpectedPolicyVersion: s.PolicyVersion})
	if saved.State != "completed" || saved.Session.PolicyVersion == s.PolicyVersion {
		t.Fatalf("effort policy was not versioned: %+v", saved)
	}
	s = *saved.Session
	var snapshot protocol.PolicySnapshot
	if err := data.Get("policies", s.ID+":"+s.PolicyVersion, &snapshot); err != nil || snapshot.Policy.ReasoningEffort != p.ReasoningEffort {
		t.Fatalf("effort snapshot: %+v %v", snapshot, err)
	}
	start := func(id string) protocol.Session {
		t.Helper()
		result := x.Handle(context.Background(), protocol.Command{ID: id, SourceID: "input", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "start", Input: []protocol.Input{{Type: "text", Text: "continue"}}, ExpectedPolicyVersion: s.PolicyVersion})
		if result.State != "completed" {
			t.Fatal(result.Error)
		}
		return *result.Session
	}
	s = start("effort-turn")
	params := lastParameters(t, fake, "turn/start")
	settings := params["collaborationMode"].(map[string]any)["settings"].(map[string]any)
	if settings["reasoning_effort"] != "future-effort" || settings["model"] != s.DefaultModel {
		t.Fatalf("next-turn settings: %+v", settings)
	}
	finishTurn(x, s)
	p.ReasoningEffort = ""
	reset := x.Handle(context.Background(), protocol.Command{ID: "effort-reset", SourceID: "input", DeviceID: s.DeviceID, SessionID: s.ID, Kind: "policy.save", Policy: &p, ExpectedPolicyVersion: s.PolicyVersion})
	if reset.State != "completed" {
		t.Fatal(reset.Error)
	}
	s = *reset.Session
	_ = start("default-effort-turn")
	settings = lastParameters(t, fake, "turn/start")["collaborationMode"].(map[string]any)["settings"].(map[string]any)
	if effort, supplied := settings["reasoning_effort"]; !supplied || effort != nil {
		t.Fatalf("default effort retained an earlier override: %+v", settings)
	}
}
