package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

func TestEmptyExecutorPreparesDefaultWorkspace(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fake := &fixtureEngine{notifications: make(chan engine.Notification, 10)}
	c := config.Config{DeviceID: "empty-device", DataDir: t.TempDir(), Executor: true}
	x, err := New(c, s, fake, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	projects := x.Projects()
	if len(projects) != 1 || projects[0].Name != "默认工作区" {
		t.Fatal("default workspace is unavailable", projects)
	}
	defaultProject := projects[0]
	expected, err := filepath.EvalSymlinks(filepath.Join(filepath.Dir(c.DataDir), "workspaces", "default"))
	if err != nil || defaultProject.Path != expected {
		t.Fatal("default workspace is not on the execution device", defaultProject, err)
	}
	rejected := x.Handle(context.Background(), protocol.Command{ID: "no-workspace", DeviceID: c.DeviceID, Kind: "create", SourceID: "client", ProjectID: ""})
	if rejected.State != "failed" {
		t.Fatal("execution allowed without a project", rejected)
	}
	root := t.TempDir()
	created := x.Handle(context.Background(), protocol.Command{ID: "first-workspace", DeviceID: c.DeviceID, Kind: "workspace.create", SourceID: "admin", Workspace: &protocol.WorkspaceRequest{Path: root, Name: "首个项目"}})
	if created.State != "completed" || len(x.Projects()) != 2 {
		t.Fatal("cannot add the first project from the workbench", created)
	}
	restored := &Executor{config: c, store: s, projectRoots: map[string]string{}}
	if err := restored.bindWorkspaces(); err != nil {
		t.Fatal(err)
	}
	if len(restored.Projects()) != 2 {
		t.Fatal("workspace catalog was not persisted", restored.Projects())
	}
	if restoredDefault, err := restored.project(defaultProject.ID); err != nil || !reflect.DeepEqual(restoredDefault, defaultProject) {
		t.Fatal("default workspace changed after restart", restoredDefault, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.methods) != 0 {
		t.Fatal("starting empty or registering a project called the engine", fake.methods)
	}
}

func TestLiveWorkspaceCreationSurvivesCatalogRebuild(t *testing.T) {
	x, s, _ := setup(t)
	root := filepath.Join(t.TempDir(), "new-project")
	command := protocol.Command{ID: "workspace-create", DeviceID: "device", Kind: "workspace.create", SourceID: "admin", Workspace: &protocol.WorkspaceRequest{Path: root, Name: "新项目", Category: "个人项目", CreateFolder: true}}
	result := x.Handle(context.Background(), command)
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	var project protocol.Project
	if err := json.Unmarshal(result.Data, &project); err != nil {
		t.Fatal(err)
	}
	repeat := x.Handle(context.Background(), command)
	if string(repeat.Data) != string(result.Data) || len(x.Projects()) != 2 {
		t.Fatal("replayed creation duplicated or changed the workspace")
	}
	updated := x.Handle(context.Background(), protocol.Command{ID: "organize", DeviceID: "device", ProjectID: project.ID, Kind: "workspace.update", SourceID: "admin", Workspace: &protocol.WorkspaceRequest{Path: "ignored", Name: "工作项目", Category: "工作"}})
	if updated.State != "completed" {
		t.Fatal(updated.Error)
	}
	restored := &Executor{config: config.Config{DeviceID: "device", Projects: x.config.Projects[:1]}, store: s, projectRoots: map[string]string{}}
	if err := restored.bindWorkspaces(); err != nil {
		t.Fatal(err)
	}
	current, err := restored.project(project.ID)
	if err != nil || current.Name != "工作项目" || current.Category != "工作" || current.Path != project.Path {
		t.Fatal("lost workspace identity or metadata", current, err)
	}
	var binding projectBinding
	if err := s.Get("project_bindings", project.ID, &binding); err != nil || binding.Path != project.Path {
		t.Fatal("workspace was rebound", err)
	}
}

func TestWorkspaceCreationDoesNotIgnoreRequestedName(t *testing.T) {
	x, _, _ := setup(t)
	project := x.Projects()[0]
	result := x.Handle(context.Background(), protocol.Command{ID: "same-folder-new-name", DeviceID: "device", Kind: "workspace.create", SourceID: "admin", Workspace: &protocol.WorkspaceRequest{Path: project.Path, Name: "另一个名字"}})
	if result.State != "failed" {
		t.Fatal("a different requested name was silently discarded", result)
	}
	if x.Projects()[0].Name != project.Name {
		t.Fatal("creating an alias unexpectedly renamed a shared workspace")
	}
}

func TestWorkspacePathsUseTheTargetMachinesHome(t *testing.T) {
	x, _, _ := setup(t)
	home := t.TempDir()
	variable := "HOME"
	if runtime.GOOS == "windows" {
		variable = "USERPROFILE"
	}
	t.Setenv(variable, home)
	if err := os.Mkdir(filepath.Join(home, "project"), 0700); err != nil {
		t.Fatal(err)
	}
	result := x.Handle(context.Background(), protocol.Command{ID: "home-project", DeviceID: "device", Kind: "workspace.create", SourceID: "admin", Workspace: &protocol.WorkspaceRequest{Path: "~/project", Name: "用户项目"}})
	if result.State != "completed" {
		t.Fatal(result.Error)
	}
	var project protocol.Project
	if err := json.Unmarshal(result.Data, &project); err != nil {
		t.Fatal(err)
	}
	actual, err := filepath.EvalSymlinks(filepath.Join(home, "project"))
	if err != nil || project.Path != actual {
		t.Fatal("directory did not resolve on the target machine", project.Path, actual, err)
	}
}

func TestAccountLimitReadsAndLogoutAreExplicitEngineCalls(t *testing.T) {
	x, _, fake := setup(t)
	for _, kind := range []string{"account.limits", "account.logout"} {
		result := x.Handle(context.Background(), protocol.Command{ID: kind, DeviceID: "device", Kind: kind, SourceID: "admin"})
		if result.State != "completed" {
			t.Fatal(result.Error)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.methods) != 2 || fake.methods[0] != "account/rateLimits/read" || fake.methods[1] != "account/logout" {
		t.Fatal(fake.methods)
	}
}

func TestTokenUsageIsProjectedIntoConversation(t *testing.T) {
	x, s, _ := setup(t)
	result := x.Handle(context.Background(), protocol.Command{ID: "context", DeviceID: "device", ProjectID: "project", Kind: "create", SourceID: "client"})
	if result.Session == nil {
		t.Fatal(result.Error)
	}
	session := *result.Session
	x.consumeOne(engine.Notification{Method: "thread/tokenUsage/updated", Params: protocol.JSON(map[string]any{"threadId": session.EngineID, "tokenUsage": map[string]any{"last": map[string]int64{"inputTokens": 12000, "outputTokens": 100, "totalTokens": 12100}, "modelContextWindow": 128000}})})
	if err := s.Get("sessions", session.ID, &session); err != nil {
		t.Fatal(err)
	}
	if session.ContextUsage == nil || session.ContextUsage.TotalTokens != 12100 || session.ContextUsage.ModelContextWindow == nil || *session.ContextUsage.ModelContextWindow != 128000 {
		t.Fatal("real token usage not retained", session.ContextUsage)
	}
}

func TestAccessChangesWaitForNextTurnAndResumeSameConversation(t *testing.T) {
	x, _, fake := setup(t)
	session := createAndStart(t, x)
	oldScope, engineID := session.ExecutionScopeID, session.EngineID
	next := session.Policy
	next.AccessMode = "read-only"
	setPolicy(&session, next)
	active, err := x.executionScope(session)
	if err != nil || active.ID() != oldScope {
		t.Fatal("a next-turn preference changed the active turn", err)
	}
	session.State, session.TurnID = "idle", ""
	if err := x.load(context.Background(), &session); err != nil {
		t.Fatal(err)
	}
	if session.EngineID != engineID || session.ExecutionScopeID == oldScope || session.ScopeAccessMode != "read-only" {
		t.Fatal("next-turn access did not apply to the existing conversation")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.methods[len(fake.methods)-1] != "thread/resume" {
		t.Fatal("the existing thread was not reconfigured")
	}
}
