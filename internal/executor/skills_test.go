package executor

import (
	"context"
	"encoding/json"
	"testing"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type skillFixture struct{ *fixtureEngine }

func (f *skillFixture) Call(ctx context.Context, method string, params any, out any) error {
	if err := f.fixtureEngine.Call(ctx, method, params, out); err != nil || method != "skills/list" {
		return err
	}
	var request struct {
		Cwds []string `json:"cwds"`
	}
	if err := json.Unmarshal(protocol.JSON(params), &request); err != nil {
		return err
	}
	return json.Unmarshal(protocol.JSON(map[string]any{"data": []any{
		map[string]any{"cwd": "/other-project", "skills": []any{map[string]any{"name": "other", "path": "/other/SKILL.md", "enabled": true}}},
		map[string]any{"cwd": request.Cwds[0], "skills": []any{
			map[string]any{"name": "pdf", "description": "Read PDF", "path": "/skills/pdf/SKILL.md", "scope": "user", "enabled": true, "private": "not-public"},
			map[string]any{"name": "disabled", "path": "/disabled/SKILL.md", "enabled": false},
		}, "errors": []any{map[string]string{"message": "One skill could not be read"}}},
	}}), out)
}

func TestSkillCatalogIsReadOnlyScopedAndExcludesDisabledSkills(t *testing.T) {
	x, data, fake := setupWithEngine(t, func(f *fixtureEngine) engine.Engine { return &skillFixture{f} })
	command := protocol.Command{ID: "skill-catalog", SourceID: "input", DeviceID: "device", ProjectID: "project", Kind: "skill.list"}
	result := x.Handle(context.Background(), command)
	var catalog protocol.SkillCatalog
	if result.State != "completed" || json.Unmarshal(result.Data, &catalog) != nil || len(catalog.Skills) != 1 || catalog.Skills[0].Name != "pdf" || len(catalog.Errors) != 1 {
		t.Fatalf("catalog projection: %+v %s", result, result.Data)
	}
	var public map[string]any
	_ = json.Unmarshal(result.Data, &public)
	if _, ok := public["skills"].([]any)[0].(map[string]any)["private"]; ok {
		t.Fatal("private metadata reached the input")
	}
	request := lastParameters(t, fake, "skills/list")
	if request["forceReload"] != true || len(request["cwds"].([]any)) != 1 {
		t.Fatalf("catalog request: %+v", request)
	}
	sessions, err := store.List[protocol.Session](data, "sessions")
	if err != nil || len(sessions) != 0 {
		t.Fatal("catalog created a conversation", err)
	}
	invalid := command
	invalid.ID, invalid.ProjectID = "wrong-skill-project", "missing"
	if x.Handle(context.Background(), invalid).State != "failed" {
		t.Fatal("queried an unconfigured project")
	}
	if err := data.Put("sessions", "other", protocol.Session{ID: "other", DeviceID: "other-device", ProjectID: "project"}); err != nil {
		t.Fatal(err)
	}
	invalid.ID, invalid.ProjectID, invalid.SessionID = "wrong-skill-session", "project", "other"
	if x.Handle(context.Background(), invalid).State != "failed" {
		t.Fatal("queried a different conversation's skills")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.methods) != 1 || fake.methods[0] != "skills/list" {
		t.Fatalf("unexpected operations: %+v", fake.methods)
	}
}
