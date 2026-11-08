package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/projectharness"
	"mewdex/internal/protocol"
)

type projectHarnessRunner struct{ command protocol.Command }

func TestProjectConfigurationRPCFixture(t *testing.T) {
	if os.Getenv("MEWDEX_HARNESS_RPC") != "1" {
		return
	}
	for _, argument := range os.Args {
		if argument == "--version" {
			fmt.Println("codex-cli " + engine.CompatibleVersion)
			os.Exit(0)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) == nil && len(request.ID) > 0 {
			fmt.Println(string(protocol.JSON(map[string]any{"id": request.ID, "result": request.Params})))
		}
	}
	os.Exit(0)
}

func TestProjectConfigurationReachesEngineApplicationContext(t *testing.T) {
	t.Setenv("MEWDEX_HARNESS_RPC", "1")
	h, state, _, _ := testHub(t)
	if err := h.Device(protocol.Device{ID: "node", Projects: []protocol.Project{{ID: "root", Path: "/work"}}}); err != nil {
		t.Fatal(err)
	}
	project, err := state.SaveHubProject(h.config.DeviceID, "hp_wire", protocol.HubProjectRequest{Name: "Wire", Directories: []protocol.ProjectDirectory{{DeviceID: "node", ProjectID: "root"}}})
	if err != nil {
		t.Fatal(err)
	}
	configuration := projectharness.Empty()
	configuration.Instructions = "WIRE_PROJECT_INSTRUCTIONS"
	if _, err = state.SaveProjectHarness(h.config.DeviceID, project.ID, protocol.ProjectHarnessUpdate{ExpectedProjectRevision: project.Revision, Configuration: configuration}); err != nil {
		t.Fatal(err)
	}
	session := protocol.Session{ID: "wire-session", ProjectID: project.ID, EngineID: "wire-thread", LastStartID: "wire-command"}
	if err = state.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rpc, err := engine.Start(ctx, config.Engine{Executable: os.Args[0], Args: []string{"-test.run=^TestProjectConfigurationRPCFixture$", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	bridge := &coordinatorEngine{Codex: rpc, coordinator: &Coordinator{hub: h, store: state}}
	if _, err = state.RemoveHubProject(h.config.DeviceID, project.ID, project.Revision); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"threadId": session.EngineID, "collaborationMode": map[string]any{"settings": map[string]any{"developer_instructions": "WIRE_TASK_INSTRUCTIONS"}}, "additionalContext": map[string]any{"retained": map[string]string{"kind": "application", "value": "existing context"}}}
	var echoed map[string]any
	if err = bridge.Call(ctx, "turn/start", params, &echoed); err != nil {
		t.Fatal(err)
	}
	additional, ok := echoed["additionalContext"].(map[string]any)
	if !ok || additional["retained"] == nil {
		t.Fatal("engine RPC lost application context")
	}
	keys := make([]string, 0, len(additional))
	for key := range additional {
		if key != "retained" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var document strings.Builder
	for _, key := range keys {
		fragment := additional[key].(map[string]any)
		_, body, _ := strings.Cut(fragment["value"].(string), "\n")
		document.WriteString(body)
	}
	text := document.String()
	if !strings.Contains(text, "WIRE_PROJECT_INSTRUCTIONS") || !strings.Contains(text, "WIRE_TASK_INSTRUCTIONS") {
		t.Fatal("engine RPC did not receive the original project and task instructions", text)
	}
}

func TestProjectConfigurationApplicationContextRetainsEveryByte(t *testing.T) {
	configuration := strings.Repeat("目录 空格 \"引号\" $(literal)\n", 20000)
	context := protocol.ProjectTurnContext{CommandID: "original-command", Instructions: configuration}
	fragments := projectApplicationContext(context, "exact task instructions")
	keys := make([]string, 0, len(fragments))
	for key := range fragments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var restored strings.Builder
	for _, key := range keys {
		fragment := fragments[key].(map[string]string)
		if fragment["kind"] != "application" || len(fragment["value"]) > 1000 || !utf8.ValidString(fragment["value"]) {
			t.Fatal("application fragment can be truncated or lose Unicode", key)
		}
		_, body, _ := strings.Cut(fragment["value"], "\n")
		restored.WriteString(body)
	}
	if !strings.HasSuffix(restored.String(), configuration+"\n\nexact task instructions") {
		t.Fatal("project instructions or literal argv changed during fragmentation")
	}
	context.CommandID = "next-command"
	for key := range projectApplicationContext(context, "exact task instructions") {
		if _, exists := fragments[key]; exists {
			t.Fatal("a subsequent turn reused earlier application context identity")
		}
	}
}

func (r *projectHarnessRunner) Handle(_ context.Context, command protocol.Command) protocol.CommandResult {
	r.command = command
	exit := 0
	job := protocol.TerminalJob{ID: command.ID, OwnerID: command.Terminal.OwnerID, ProjectID: command.ProjectID, State: "completed", ExitCode: &exit}
	return protocol.CommandResult{CommandID: command.ID, State: "completed", Data: protocol.JSON(job)}
}

func TestProjectConfigurationAccessAndPerTurnVersionRemainScoped(t *testing.T) {
	h, state, server, admin := testHub(t)
	node := protocol.Device{ID: h.config.DeviceID, Name: "Machine", Projects: []protocol.Project{{ID: "directory", Name: "Root", Path: "/work"}}}
	if err := h.Device(node); err != nil {
		t.Fatal(err)
	}
	project, err := state.SaveHubProject(h.config.DeviceID, "hp_visible", protocol.HubProjectRequest{Name: "Visible", Directories: []protocol.ProjectDirectory{{DeviceID: node.ID, ProjectID: "directory"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.Put("hub_projects", "hp_private", protocol.Project{ID: "hp_private", Name: "Private"}); err != nil {
		t.Fatal(err)
	}
	input := protocol.ProjectHarnessUpdate{ExpectedProjectRevision: project.Revision, Configuration: projectharness.Empty()}
	input.Configuration.Instructions = "FIRST_VERSION_MARKER"
	endpoint := "/v1/projects/" + project.ID + "/harness"
	response := request(t, server.URL, admin, "PUT", endpoint, input)
	var first protocol.ProjectHarnessState
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&first) != nil || first.Revision != 1 {
		t.Fatal("project configuration was not saved", response.StatusCode, first)
	}
	code, err := state.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: h.coordinatorID, ProjectIDs: []string{project.ID}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client, _, err := state.Pair(code, "project reader", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []struct {
		method, path string
		status       int
	}{
		{"GET", endpoint, 200},
		{"GET", endpoint + "/history", 200},
		{"PUT", endpoint, 403},
		{"GET", "/v1/projects/hp_private/harness", 403},
		{"GET", "/v1/projects/hp_private/harness/history", 403},
	} {
		if response = request(t, server.URL, client, operation.method, operation.path, input); response.StatusCode != operation.status {
			t.Fatal(operation, response.StatusCode)
		}
	}
	bridge := &coordinatorEngine{coordinator: &Coordinator{hub: h, store: state}}
	session := protocol.Session{ID: "session", DeviceID: h.coordinatorID, ProjectID: project.ID, EngineID: "thread", LastStartID: "first-command", TurnID: "turn", State: "running", Policy: protocol.Policy{AccessMode: "read-only"}}
	if err = state.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	captured, err := bridge.prepareProjectTurn(session)
	if err != nil || captured.Harness.Version != first.Version || !strings.Contains(captured.Instructions, "FIRST_VERSION_MARKER") {
		t.Fatal("turn did not capture project configuration", captured, err)
	}
	input.ExpectedRevision = 1
	input.Configuration.Instructions = "SECOND_VERSION_MARKER"
	if response = request(t, server.URL, admin, "PUT", endpoint, input); response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	replayed, err := bridge.prepareProjectTurn(session)
	if err != nil || replayed.Instructions != captured.Instructions || replayed.CreatedAt != captured.CreatedAt {
		t.Fatal("retry adopted a later project configuration", err)
	}
	next := session
	next.LastStartID = "second-command"
	latest, err := bridge.prepareProjectTurn(next)
	if err != nil || latest.Harness.Revision != 2 || !strings.Contains(latest.Instructions, "SECOND_VERSION_MARKER") {
		t.Fatal("next turn did not adopt the new version", latest, err)
	}
	index := 0
	call := remoteToolCall{ThreadID: session.EngineID, TurnID: session.TurnID, CallID: "tool", Tool: "mewdex_exec"}
	call.Arguments.Directory, call.Arguments.Command = &index, []string{"inspect"}
	runner := &projectHarnessRunner{}
	h.SetLocal(runner)
	if _, err = bridge.executeTool(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	if runner.command.Terminal.HarnessVersion != first.Version || runner.command.Terminal.HarnessRevision != 1 || runner.command.Terminal.AccessMode != "read-only" {
		t.Fatal("tool call lost its original version or permissions", runner.command)
	}
	var target terminalTarget
	if err = state.Get("terminal_targets", runner.command.ID, &target); err != nil || target.Execution.HarnessVersion != first.Version {
		t.Fatal("historical attribution did not retain the configuration version", target, err)
	}
	response = request(t, server.URL, client, "GET", endpoint+"/turn?sessionId="+session.ID, nil)
	var observed protocol.ProjectTurnContext
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&observed) != nil || observed.Harness.Version != first.Version {
		t.Fatal("input could not inspect the active turn's original configuration", response.StatusCode)
	}
	if response = request(t, server.URL, client, "GET", "/v1/projects/hp_private/harness/turn?sessionId="+session.ID, nil); response.StatusCode != 403 {
		t.Fatal("turn inspection crossed a project boundary")
	}
	inspection := protocol.CommandResult{State: "completed", Session: &session, Data: protocol.JSON(protocol.PromptInspection{ProjectID: project.ID})}
	if err = bridge.coordinator.addProjectConfigurationSource(protocol.Command{}, &inspection); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(inspection.Data), "SECOND_VERSION_MARKER") || !strings.Contains(string(inspection.Data), "FIRST_VERSION_MARKER") {
		t.Fatal("prompt inspector showed a later configuration as the active turn")
	}
}

func TestProjectConfigurationEventsDoNotRevealOtherProjects(t *testing.T) {
	h, _, _, _ := testHub(t)
	id := protocol.Identity{Role: "client", Access: protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: h.coordinatorID, ProjectIDs: []string{"allowed"}}}}}
	for _, project := range []string{"allowed", "private"} {
		event := protocol.Event{Kind: "project.harness.updated", Payload: protocol.JSON(protocol.ProjectHarnessRevision{ProjectID: project})}
		if h.visibleEvent(id, event) != (project == "allowed") {
			t.Fatal("project configuration event crossed an access boundary")
		}
	}
}
