package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mewdex/internal/attachments"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"mewdex/internal/transport"
)

// The subprocess fixture implements only --version and sandbox argv forwarding.
// Any attempt to start an execution-node model server fails this test.
func TestCoordinatorSandboxFixture(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		return
	}
	arguments := os.Args[separator+1:]
	if len(arguments) == 1 && arguments[0] == "--version" {
		fmt.Println("codex-cli " + engine.CompatibleVersion)
		os.Exit(0)
	}
	if len(arguments) == 1 && arguments[0] == "payload" {
		file, err := os.OpenFile("executed.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(2)
		}
		_, err = file.WriteString("once\n")
		file.Close()
		if err != nil {
			os.Exit(2)
		}
		fmt.Println("execution-node-output")
		os.Exit(0)
	}
	if len(arguments) == 1 && arguments[0] == "slow-payload" {
		file, err := os.OpenFile("slow-executed.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(2)
		}
		_, err = file.WriteString("once\n")
		file.Close()
		if err != nil {
			os.Exit(2)
		}
		fmt.Println("background command started")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	cwd, commandIndex, sandbox, profile := "", -1, false, false
	for index, argument := range arguments {
		switch argument {
		case "sandbox":
			sandbox = true
		case "--permission-profile":
			profile = true
		case "-C":
			if index+1 < len(arguments) {
				cwd = arguments[index+1]
			}
		case "--":
			if commandIndex < 0 {
				commandIndex = index + 1
			}
		}
	}
	if !sandbox || !profile || commandIndex < 0 || commandIndex >= len(arguments) {
		os.Exit(3)
	}
	command := exec.Command(arguments[commandIndex], arguments[commandIndex+1:]...)
	command.Dir, command.Stdout, command.Stderr = cwd, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	os.Exit(0)
}

func TestHubProjectRoutesOneConversationToMultipleModelFreeNodes(t *testing.T) {
	h, state, server, admin := testHub(t)
	h.coordinatorID = "local.hub"
	h.coordinator = &Coordinator{hub: h, store: state}
	assets, err := attachments.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.SetAttachments(assets)
	if err := h.Device(protocol.Device{ID: h.coordinatorID, Roles: []string{"coordinator", "hub", "executor"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	directories := []protocol.ProjectDirectory{}
	roots := []string{}
	for _, id := range []string{"windows-node", "mac-node"} {
		root := t.TempDir()
		projectRoot := filepath.Join(root, "project")
		if err := os.Mkdir(projectRoot, 0700); err != nil {
			t.Fatal(err)
		}
		code, err := state.Invite("executor", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		token, _, err := state.Pair(code, id, id)
		if err != nil {
			t.Fatal(err)
		}
		tokenPath := filepath.Join(root, "token")
		if err = os.WriteFile(tokenPath, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		c := config.Config{DeviceID: id, Name: id, DataDir: filepath.Join(root, "data"), HubURL: server.URL, NodeTokenFile: tokenPath, Projects: []protocol.Project{{ID: "directory", Name: id, Path: projectRoot}}, Engine: config.Engine{Executable: os.Args[0], Args: []string{"-test.run=^TestCoordinatorSandboxFixture$", "--"}}}
		s, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
		if err != nil {
			t.Fatal(err)
		}
		x, err := executor.NewTerminal(ctx, c, s)
		if err != nil {
			s.Close()
			t.Fatal(err)
		}
		x.SetFilePublisher(transport.FilePublisher(c))
		node := &transport.Node{Config: c, Store: s, Executor: x, Device: protocol.Device{ID: id, HostID: store.Hash(id), Name: id, Roles: []string{"executor"}, Projects: x.Projects(), Capabilities: []string{"hub-execution", "workspace-management"}, ProtocolVersion: protocol.Version}}
		nodeCtx, nodeCancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- node.Run(nodeCtx) }()
		t.Cleanup(func() { nodeCancel(); <-done; x.Close(); s.Close() })
		for {
			h.mu.Lock()
			online := h.peers[id] != nil
			h.mu.Unlock()
			if online {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("execution node did not connect")
			case <-time.After(10 * time.Millisecond):
			}
		}
		directories = append(directories, protocol.ProjectDirectory{DeviceID: id, ProjectID: "directory", Path: projectRoot, Name: id})
		roots = append(roots, projectRoot)
	}
	response := request(t, server.URL, admin, "PUT", "/v1/projects/hp_sott", protocol.HubProjectRequest{Name: "Sott", Directories: directories})
	if response.StatusCode != http.StatusOK {
		t.Fatal("project creation failed", response.Status)
	}
	if err := state.Put("sessions", "hub-session", protocol.Session{ID: "hub-session", DeviceID: h.coordinatorID, ProjectID: "hp_sott", EngineID: "hub-thread", TurnID: "turn", State: "running"}); err != nil {
		t.Fatal(err)
	}
	bridge := &coordinatorEngine{coordinator: &Coordinator{hub: h, store: state}}
	first := 0
	invalidYield := remoteToolCall{ThreadID: "hub-thread", TurnID: "turn", CallID: "invalid-yield", Tool: "mewdex_exec"}
	invalidYield.Arguments.Directory, invalidYield.Arguments.YieldMS = &first, 20001
	invalidYield.Arguments.Command = []string{os.Args[0], "-test.run=^TestCoordinatorSandboxFixture$", "--", "payload"}
	if _, err := bridge.executeTool(ctx, invalidYield); err == nil {
		t.Fatal("invalid yield started an execution job")
	}
	for index, root := range roots {
		call := remoteToolCall{ThreadID: "hub-thread", TurnID: "turn", CallID: fmt.Sprint("call-", index), Tool: "mewdex_exec"}
		call.Arguments.Directory, call.Arguments.YieldMS = &index, 2000
		call.Arguments.Command = []string{os.Args[0], "-test.run=^TestCoordinatorSandboxFixture$", "--", "payload"}
		for repeat := 0; repeat < 2; repeat++ {
			data, err := bridge.executeTool(ctx, call)
			if err != nil {
				t.Fatal(err)
			}
			var job protocol.TerminalJob
			if json.Unmarshal(data, &job) != nil {
				t.Fatalf("invalid execution result %s", data)
			}
			for observation := 0; job.State == "running"; observation++ {
				wait := remoteToolCall{ThreadID: "hub-thread", TurnID: "turn", CallID: fmt.Sprintf("wait-%d-%d-%d", index, repeat, observation), Tool: "mewdex_wait"}
				wait.Arguments.Directory, wait.Arguments.JobID, wait.Arguments.YieldMS = &index, job.ID, 2000
				data, err = bridge.executeTool(ctx, wait)
				if err != nil || json.Unmarshal(data, &job) != nil {
					t.Fatalf("execution observation failed %s %v", data, err)
				}
			}
			if job.State != "completed" || !strings.Contains(job.Stdout, "execution-node-output") {
				t.Fatalf("unexpected execution result %s", data)
			}
			var target terminalTarget
			if err := state.Get("terminal_targets", job.ID, &target); err != nil || target.Execution == nil || target.Execution.DeviceID != directories[index].DeviceID || target.Execution.DeviceName != directories[index].Name || target.Execution.Workspace != root {
				t.Fatal("execution target was not captured at dispatch", target, err)
			}
		}
		data, err := os.ReadFile(filepath.Join(root, "executed.txt"))
		if err != nil || string(data) != "once\n" {
			t.Fatal("duplicate tool delivery executed again", string(data), err)
		}
	}
	invalid := remoteToolCall{ThreadID: "hub-thread", TurnID: "turn", CallID: "invalid", Tool: "mewdex_exec"}
	index := len(directories)
	invalid.Arguments.Directory = &index
	if _, err := bridge.executeTool(ctx, invalid); err == nil {
		t.Fatal("a directory outside the Hub project was dispatched")
	}
	if response = request(t, server.URL, admin, "POST", "/v1/commands", protocol.Command{ID: "bypass", DeviceID: "windows-node", ProjectID: "directory", Kind: "terminal.exec", Terminal: &protocol.TerminalRequest{OwnerID: "hub-session:turn", Command: []string{"ignored"}}}); response.StatusCode != http.StatusForbidden {
		t.Fatal("input bypassed Hub dispatch", response.Status)
	}
	for _, kind := range []string{"file.list", "file.export"} {
		selected := 1
		path := "."
		if kind == "file.export" {
			path = "executed.txt"
		}
		id := "test-" + kind
		response = request(t, server.URL, admin, "POST", "/v1/commands", protocol.Command{ID: id, DeviceID: h.coordinatorID, SessionID: "hub-session", Kind: kind, File: &protocol.FileRequest{Path: path, Directory: &selected}})
		if response.StatusCode != http.StatusAccepted {
			t.Fatal("Hub file command rejected", response.Status)
		}
		var result protocol.CommandResult
		for {
			result, err = state.Result(id)
			if err != nil {
				t.Fatal(err)
			}
			if result.State != "accepted" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("file response timed out")
			case <-time.After(10 * time.Millisecond):
			}
		}
		if result.State != "completed" {
			t.Fatal("cross-node file operation failed", result.Error)
		}
		if kind == "file.export" {
			var output protocol.OutputFile
			if json.Unmarshal(result.Data, &output) != nil || output.DeviceID != h.coordinatorID || output.ProjectID != "hp_sott" || output.Directory == nil || output.Directory.DeviceID != "mac-node" {
				t.Fatal("file snapshot left the Hub conversation", string(result.Data))
			}
		}
	}
	slow := remoteToolCall{ThreadID: "hub-thread", TurnID: "turn", CallID: "background", Tool: "mewdex_exec"}
	selected := 0
	slow.Arguments.Directory, slow.Arguments.YieldMS = &selected, 10
	slow.Arguments.Command = []string{os.Args[0], "-test.run=^TestCoordinatorSandboxFixture$", "--", "slow-payload"}
	data, err := bridge.executeTool(ctx, slow)
	if err != nil {
		t.Fatal(err)
	}
	var running protocol.TerminalJob
	if json.Unmarshal(data, &running) != nil || running.State != "running" {
		t.Fatal("background job did not start", string(data))
	}
	for {
		if marker, err := os.ReadFile(filepath.Join(roots[0], "slow-executed.txt")); err == nil && string(marker) == "once\n" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("background process did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	h.mu.Lock()
	previousPeer := h.peers[directories[0].DeviceID]
	h.mu.Unlock()
	if err := previousPeer.conn.CloseNow(); err != nil {
		t.Fatal(err)
	}
	for {
		h.mu.Lock()
		currentPeer := h.peers[directories[0].DeviceID]
		h.mu.Unlock()
		if currentPeer != nil && currentPeer != previousPeer {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("executor did not reconnect during its background command")
		case <-time.After(10 * time.Millisecond):
		}
	}
	data, err = bridge.executeTool(ctx, slow)
	var repeated protocol.TerminalJob
	if err != nil || json.Unmarshal(data, &repeated) != nil || repeated.ID != running.ID {
		t.Fatal("reconnection lost the original execution receipt", string(data), err)
	}
	poll := remoteToolCall{ThreadID: slow.ThreadID, TurnID: slow.TurnID, CallID: "after-reconnect", Tool: "mewdex_wait"}
	poll.Arguments.Directory, poll.Arguments.JobID = &selected, running.ID
	data, err = bridge.executeTool(ctx, poll)
	if err != nil || json.Unmarshal(data, &repeated) != nil || repeated.State != "running" || !strings.Contains(repeated.Stdout, "background command started") {
		t.Fatal("reconnected execution lost its running process or output", string(data), err)
	}
	if marker, err := os.ReadFile(filepath.Join(roots[0], "slow-executed.txt")); err != nil || string(marker) != "once\n" {
		t.Fatal("reconnection or duplicate delivery executed the process again", string(marker), err)
	}
	if err = h.stopRemoteOwner(ctx, "hub-session"); err != nil {
		t.Fatal("Hub failed to stop its remote processes", err)
	}
	slow.CallID = "late-after-stop"
	if _, err = bridge.executeTool(ctx, slow); err == nil || !strings.Contains(err.Error(), "此回合已停止") {
		t.Fatal("a late instruction crossed the persisted stop barrier", err)
	}
	events, err := state.Events(0, "hub-session", 100)
	if err != nil || len(events) < 2 {
		t.Fatal("execution output did not reach the Hub conversation", err)
	}
	for _, event := range events {
		var payload struct {
			Item struct {
				Type      string                    `json:"type"`
				Execution *protocol.ExecutionTarget `json:"executionTarget"`
			} `json:"item"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Item.Type == "commandExecution" && (payload.Item.Execution == nil || payload.Item.Execution.DeviceID == "" || payload.Item.Execution.DeviceName == "") {
			t.Fatal("remote command output lost its execution target", string(event.Payload))
		}
	}
}

func TestExecutionTargetSnapshotSurvivesRenamesAndLegacyReceipts(t *testing.T) {
	h, state, _, _ := testHub(t)
	h.coordinatorID = "local.hub"
	if err := state.Put("sessions", "session", protocol.Session{ID: "session", DeviceID: h.coordinatorID, EngineID: "thread"}); err != nil {
		t.Fatal(err)
	}
	device := protocol.Device{ID: "node", Name: "Original machine", Platform: "windows/amd64", Projects: []protocol.Project{{ID: "directory", Name: "Original project", Path: `D:\original`}}}
	if err := state.Put("devices", device.ID, device); err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []bool{false, true} {
		id := fmt.Sprintf("job-%t", legacy)
		command := protocol.Command{ID: id, DeviceID: device.ID, ProjectID: "directory", Kind: "terminal.exec", Terminal: &protocol.TerminalRequest{OwnerID: "session:turn", Command: []string{"build"}, AccessMode: "read-only"}}
		if _, _, err := state.Claim(command); err != nil {
			t.Fatal(err)
		}
		target := terminalTarget{OwnerID: command.Terminal.OwnerID, DeviceID: device.ID, ProjectID: command.ProjectID, JobID: id, State: "unknown"}
		if !legacy {
			target.Execution = h.executionTarget(command)
		}
		if err := state.Put("terminal_targets", id, target); err != nil {
			t.Fatal(err)
		}
		device.Name, device.Projects[0].Name, device.Projects[0].Path = "Renamed machine", "Renamed project", `D:\changed`
		if err := state.Put("devices", device.ID, device); err != nil {
			t.Fatal(err)
		}
		job := protocol.TerminalJob{ID: id, OwnerID: target.OwnerID, ProjectID: target.ProjectID, Cwd: `D:\original\src`, State: "completed", UpdatedAt: protocol.Now()}
		if err := h.recordTerminalJob(device.ID, "result-"+id, job); err != nil {
			t.Fatal(err)
		}
	}
	events, err := state.Events(0, "session", 10)
	if err != nil || len(events) != 2 {
		t.Fatal("expected both new and legacy output events", events, err)
	}
	for _, event := range events {
		var payload struct {
			Item struct {
				ID        string                    `json:"id"`
				Execution *protocol.ExecutionTarget `json:"executionTarget"`
			} `json:"item"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		target := payload.Item.Execution
		if target == nil || target.DeviceID != "node" || target.ProjectID != "directory" || target.AccessMode != "read-only" {
			t.Fatal("command identity or permission was lost", target)
		}
		if payload.Item.ID == "job-false" && (target.DeviceName != "Original machine" || target.ProjectName != "Original project" || target.Workspace != `D:\original`) {
			t.Fatal("a rename rewrote historical attribution", target)
		}
		if payload.Item.ID == "job-true" && (target.DeviceName != "" || target.ProjectName != "" || target.Workspace != "") {
			t.Fatal("legacy output inherited present-day metadata", target)
		}
	}
}

func TestHubStopBeforeFirstDispatchRejectsLateInstructions(t *testing.T) {
	h, state, _, _ := testHub(t)
	runner := collaborationRunner{calls: make(chan protocol.Command, 1)}
	h.SetLocal(runner)
	session := protocol.Session{ID: "session", TurnID: "turn", State: "running"}
	if err := state.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	if err := h.stopRemoteOwner(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	command := protocol.Command{ID: "late", DeviceID: h.config.DeviceID, ProjectID: "project", Kind: "terminal.exec", SourceID: "hub", Terminal: &protocol.TerminalRequest{OwnerID: "session:turn", Command: []string{"ignored"}}}
	result, err := h.runTerminal(context.Background(), command)
	if err == nil || result.State != "failed" || result.Error != "此回合已停止" {
		t.Fatal("Hub accepted a command after the stop barrier", result, err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("stopped command reached the execution node")
	}
}

type failedTerminalRunner struct{}

func (failedTerminalRunner) Handle(_ context.Context, c protocol.Command) protocol.CommandResult {
	return protocol.CommandResult{CommandID: c.ID, State: "failed", Error: "execution status unavailable"}
}

func TestFailedTerminalPollRetainsOutstandingExecution(t *testing.T) {
	h, state, _, _ := testHub(t)
	h.SetLocal(failedTerminalRunner{})
	target := terminalTarget{OwnerID: "session:turn", DeviceID: h.config.DeviceID, ProjectID: "project", JobID: "job", State: "running"}
	if err := state.Put("terminal_targets", target.JobID, target); err != nil {
		t.Fatal(err)
	}
	command := protocol.Command{ID: "poll", DeviceID: target.DeviceID, ProjectID: target.ProjectID, Kind: "terminal.wait", SourceID: "hub", Terminal: &protocol.TerminalRequest{OwnerID: target.OwnerID, JobID: target.JobID}}
	if _, err := h.runTerminal(context.Background(), command); err == nil {
		t.Fatal("failed polling did not report its error")
	}
	if err := state.Get("terminal_targets", target.JobID, &target); err != nil || target.State != "running" {
		t.Fatal("a failed status read discarded an outstanding execution", target, err)
	}
}

func TestLocalExecutionOutboxBeforeCoordinatorStartup(t *testing.T) {
	h, state, _, _ := testHub(t)
	session := protocol.Session{ID: "session", DeviceID: "local.hub", EngineID: "thread"}
	if err := state.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	command := protocol.Command{ID: "job", DeviceID: h.config.DeviceID, ProjectID: "directory", Kind: "terminal.exec", Terminal: &protocol.TerminalRequest{OwnerID: "session:turn"}}
	if _, _, err := state.Claim(command); err != nil {
		t.Fatal(err)
	}
	target := terminalTarget{OwnerID: "session:turn", DeviceID: command.DeviceID, ProjectID: command.ProjectID, JobID: command.ID, State: "unknown"}
	if err := state.Put("terminal_targets", command.ID, target); err != nil {
		t.Fatal(err)
	}
	job := protocol.TerminalJob{ID: command.ID, OwnerID: target.OwnerID, ProjectID: target.ProjectID, State: "completed", Stdout: "persisted output", UpdatedAt: protocol.Now()}
	if _, err := h.Ingest(protocol.Event{ID: "replayed-output", DeviceID: command.DeviceID, Kind: "terminal.job.updated", Payload: protocol.JSON(job), CreatedAt: job.UpdatedAt}); err != nil {
		t.Fatal("replaying a local execution outbox blocked Hub startup", err)
	}
	events, err := state.Events(0, session.ID, 10)
	if err != nil || len(events) != 1 || !strings.Contains(string(events[0].Payload), `"turnId":"turn"`) {
		t.Fatal("replayed output lost its original Hub turn", events, err)
	}
}

func TestHubLocalFileSnapshotUsesSeparateExecutionCache(t *testing.T) {
	h, state, server, admin := testHub(t)
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	if err := os.Mkdir(projectRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "report.txt"), []byte("local snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Config{DeviceID: h.config.DeviceID, DataDir: filepath.Join(root, "data"), Projects: []protocol.Project{{ID: "directory", Name: "local", Path: projectRoot}}, Engine: config.Engine{Executable: os.Args[0], Args: []string{"-test.run=^TestCoordinatorSandboxFixture$", "--"}}}
	s, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err := executor.NewTerminal(context.Background(), c, s)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	h.SetLocal(x)
	x.SetFilePublisher(h.PublishFileExport)
	assets, err := attachments.New(filepath.Join(c.DataDir, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	h.SetAttachments(assets)
	h.coordinatorID = "local.hub"
	coordinator := &Coordinator{hub: h, store: state}
	h.coordinator = coordinator
	if err = h.Device(protocol.Device{ID: c.DeviceID, Projects: c.Projects, Roles: []string{"executor"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SaveHubProject(c.DeviceID, "hp_local", protocol.HubProjectRequest{Name: "local", Directories: []protocol.ProjectDirectory{{DeviceID: c.DeviceID, ProjectID: "directory"}}}); err != nil {
		t.Fatal(err)
	}
	if err = state.Put("sessions", "local-session", protocol.Session{ID: "local-session", DeviceID: h.coordinatorID, ProjectID: "hp_local"}); err != nil {
		t.Fatal(err)
	}
	var actor protocol.Identity
	response := request(t, server.URL, admin, "GET", "/v1/me", nil)
	if err = json.NewDecoder(response.Body).Decode(&actor); err != nil {
		t.Fatal(err)
	}
	command := protocol.Command{ID: "local-export", DeviceID: h.coordinatorID, SessionID: "local-session", Kind: "file.export", SourceID: actor.ID, CreatedAt: protocol.Now(), File: &protocol.FileRequest{Path: "report.txt"}}
	if _, _, err = state.Claim(command); err != nil {
		t.Fatal(err)
	}
	result := coordinator.Handle(context.Background(), command)
	if result.State != "completed" {
		t.Fatal("local execution cache collided with Hub attachment ownership", result.Error)
	}
	var output protocol.OutputFile
	if json.Unmarshal(result.Data, &output) != nil || output.Attachment.TargetDeviceID != h.coordinatorID {
		t.Fatal(string(result.Data))
	}
}
