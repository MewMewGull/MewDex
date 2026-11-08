package hub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mewdex/internal/computeruse"
	"mewdex/internal/config"
	"mewdex/internal/executor"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
	"mewdex/internal/transport"
)

type computerRunnerFixture struct{ calls []protocol.Command }

func (r *computerRunnerFixture) Handle(_ context.Context, command protocol.Command) protocol.CommandResult {
	r.calls = append(r.calls, command)
	session := protocol.ComputerSession{ID: command.Computer.SessionID, DeviceID: command.DeviceID, ProjectID: command.ProjectID, OwnerID: command.Computer.OwnerID, InstanceID: "fixture", State: "ready", Revision: 2}
	if command.Kind == "computer.request" {
		session.ID, session.State = command.ID, "pending"
	}
	if command.Kind == "computer.stop" {
		session.State = "stopped"
		session.Revision = 10
	}
	result := protocol.ComputerResult{Status: protocol.ComputerStatus{Supported: true, Session: &session}}
	if command.Kind == "computer.observe" {
		result.Observation = &protocol.ComputerObservation{ID: "observation", Image: "aW1hZ2U=", MimeType: "image/jpeg", Width: 100, Height: 100}
	}
	return protocol.CommandResult{State: "completed", Data: protocol.JSON(result)}
}

type routedComputerDriver struct{ actions atomic.Int32 }
type routedComputerReservation struct{ interrupted chan struct{} }

func (r *routedComputerReservation) Close() error                 { return nil }
func (r *routedComputerReservation) Interrupted() <-chan struct{} { return r.interrupted }
func (d *routedComputerDriver) Available() (bool, string)         { return true, "" }
func (d *routedComputerDriver) Windows(context.Context) ([]protocol.ComputerWindow, error) {
	return []protocol.ComputerWindow{{ID: "fixture-window", ProcessID: 7, Title: "Isolated Computer Use fixture"}}, nil
}
func (d *routedComputerDriver) Reserve(context.Context) (computeruse.Reservation, error) {
	return &routedComputerReservation{interrupted: make(chan struct{})}, nil
}
func (d *routedComputerDriver) Observe(context.Context, protocol.ComputerWindow) (protocol.ComputerObservation, computeruse.Bounds, error) {
	picture := image.NewRGBA(image.Rect(0, 0, 20, 20))
	picture.Set(0, 0, color.White)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, nil); err != nil {
		return protocol.ComputerObservation{}, computeruse.Bounds{}, err
	}
	return protocol.ComputerObservation{Image: base64.StdEncoding.EncodeToString(encoded.Bytes()), MimeType: "image/jpeg", Width: 20, Height: 20}, computeruse.Bounds{Width: 20, Height: 20}, nil
}
func (d *routedComputerDriver) Act(context.Context, protocol.ComputerWindow, protocol.ComputerObservation, computeruse.Bounds, protocol.ComputerAction) error {
	d.actions.Add(1)
	return nil
}

func TestComputerControlTraversesPairedWebSocketAndDisconnectRevokesGrant(t *testing.T) {
	h, state, server, _ := testHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	code, err := state.Invite("executor", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := state.Pair(code, "fixture node", "computer-node")
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(root, "node-token")
	if err = os.WriteFile(tokenPath, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Config{DeviceID: "computer-node", Name: "Fixture node", DataDir: filepath.Join(root, "data"), HubURL: server.URL, NodeTokenFile: tokenPath, Projects: []protocol.Project{{ID: "root", Name: "Fixture", Path: root}}, Engine: config.Engine{Executable: os.Args[0], Args: []string{"-test.run=^TestCoordinatorSandboxFixture$", "--"}}}
	database, err := store.Open(filepath.Join(c.DataDir, "executor.db"))
	if err != nil {
		t.Fatal(err)
	}
	driver := &routedComputerDriver{}
	x, err := executor.NewTerminalWithOptions(ctx, c, database, executor.TerminalOptions{ComputerDriver: driver})
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	node := &transport.Node{Config: c, Store: database, Executor: x, Device: protocol.Device{ID: c.DeviceID, Name: c.Name, Roles: []string{"executor"}, Projects: x.Projects(), Capabilities: []string{"hub-execution", "computer-use-v1"}, ProtocolVersion: protocol.Version}}
	nodeCtx, nodeCancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- node.Run(nodeCtx) }()
	t.Cleanup(func() { nodeCancel(); <-done; x.Close(); database.Close() })
	for {
		h.mu.Lock()
		online := h.peers[c.DeviceID] != nil
		h.mu.Unlock()
		if online {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("node did not connect")
		case <-time.After(10 * time.Millisecond):
		}
	}
	project, err := state.SaveHubProject(h.config.DeviceID, "hp_cua_transport", protocol.HubProjectRequest{Name: "Computer transport", Directories: []protocol.ProjectDirectory{{DeviceID: c.DeviceID, ProjectID: "root"}}})
	if err != nil {
		t.Fatal(err)
	}
	session := protocol.Session{ID: "computer-conversation", DeviceID: h.coordinatorID, ProjectID: project.ID, EngineID: "computer-thread", TurnID: "computer-turn", State: "running", Policy: protocol.Policy{AccessMode: "workspace"}}
	if err = state.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	bridge := &coordinatorEngine{coordinator: &Coordinator{hub: h, store: state}}
	index := 0
	tool := remoteToolCall{ThreadID: session.EngineID, TurnID: session.TurnID, CallID: "request", Tool: "mewdex_computer"}
	tool.Arguments.Directory, tool.Arguments.Operation, tool.Arguments.Reason = &index, "request", "Operate only the isolated fixture"
	call := func() protocol.ComputerResult {
		t.Helper()
		data, err := bridge.executeTool(ctx, tool)
		if err != nil {
			t.Fatal(err)
		}
		var result protocol.ComputerResult
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	requested := call()
	if requested.Status.Session.State != "pending" || driver.actions.Load() != 0 {
		t.Fatal(requested)
	}
	windows, _ := driver.Windows(ctx)
	if _, err = computeruse.Approve(database, protocol.ComputerApproval{SessionID: requested.Status.Session.ID, ExpectedRevision: requested.Status.Session.Revision, WindowID: "fixture-window", DurationSeconds: 60}, windows); err != nil {
		t.Fatal(err)
	}
	tool.CallID, tool.Arguments.Operation, tool.Arguments.SessionID = "observe", "observe", requested.Status.Session.ID
	observed := call()
	if observed.Observation == nil || observed.Observation.Image == "" {
		t.Fatal(observed)
	}
	tool.CallID, tool.Arguments.Operation = "action", "act"
	tool.Arguments.ObservationID, tool.Arguments.Action = observed.Observation.ID, &protocol.ComputerAction{Kind: "type", Text: "literal fixture text"}
	first, replay := call(), call()
	if driver.actions.Load() != 1 || string(protocol.JSON(first)) != string(protocol.JSON(replay)) {
		t.Fatal("transport retry repeated UI input")
	}
	h.mu.Lock()
	peer := h.peers[c.DeviceID]
	h.mu.Unlock()
	peer.cancel()
	_ = peer.conn.CloseNow()
	for {
		current, err := database.ComputerSession()
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "stopped" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("disconnect retained window grant", current)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if driver.actions.Load() != 1 {
		t.Fatal("disconnect replayed native input")
	}
}

func TestComputerRoutingScopeReadOnlyStopAndImageResponse(t *testing.T) {
	h, database, server, admin := testHub(t)
	runner := &computerRunnerFixture{}
	h.SetLocal(runner)
	if err := h.Device(protocol.Device{ID: h.config.DeviceID, Capabilities: []string{"computer-use-v1", "hub-execution"}, Projects: []protocol.Project{{ID: "root", Path: "/fixture"}}}); err != nil {
		t.Fatal(err)
	}
	project, err := database.SaveHubProject(h.config.DeviceID, "hp_computer", protocol.HubProjectRequest{Name: "Computer", Directories: []protocol.ProjectDirectory{{DeviceID: h.config.DeviceID, ProjectID: "root"}}})
	if err != nil {
		t.Fatal(err)
	}
	session := protocol.Session{ID: "conversation", DeviceID: h.coordinatorID, ProjectID: project.ID, EngineID: "thread", TurnID: "turn", State: "running", Policy: protocol.Policy{AccessMode: "read-only"}}
	if err = database.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	bridge := &coordinatorEngine{coordinator: &Coordinator{hub: h, store: database}}
	index := 0
	tool := remoteToolCall{ThreadID: session.EngineID, TurnID: session.TurnID, CallID: "request", Tool: "mewdex_computer"}
	tool.Arguments.Directory, tool.Arguments.Operation, tool.Arguments.Reason = &index, "request", "Owned window test"
	data, err := bridge.executeTool(context.Background(), tool)
	if err != nil {
		t.Fatal(err)
	}
	var result protocol.ComputerResult
	if err = json.Unmarshal(data, &result); err != nil || result.Status.Session.State != "pending" {
		t.Fatal(string(data), err)
	}
	control := result.Status.Session.ID
	tool.CallID, tool.Arguments.Operation, tool.Arguments.SessionID = "act", "act", control
	tool.Arguments.Action = &protocol.ComputerAction{Kind: "type", Text: "input"}
	if _, err = bridge.executeTool(context.Background(), tool); err == nil || len(runner.calls) != 1 {
		t.Fatal("read-only policy permitted window input", err)
	}
	tool.CallID, tool.Arguments.Operation, tool.Arguments.Action = "observe", "observe", nil
	data, err = bridge.executeTool(context.Background(), tool)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Success bool                `json:"success"`
		Content []map[string]string `json:"contentItems"`
	}
	if err = json.Unmarshal(computerToolResponse(tool.Tool, data, nil), &output); err != nil || !output.Success || len(output.Content) != 2 || output.Content[1]["imageUrl"] != "data:image/jpeg;base64,aW1hZ2U=" {
		t.Fatal(output, err)
	}
	endpoint := "/v1/projects/" + project.ID + "/computer?sessionId=" + session.ID + "&directory=0"
	if response := request(t, server.URL, admin, "GET", endpoint, nil); response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	code, err := database.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: h.coordinatorID, ProjectIDs: []string{"other"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := database.Pair(code, "other project", "")
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, server.URL, token, "GET", endpoint, nil); response.StatusCode != 403 {
		t.Fatal("status crossed project scope", response.StatusCode)
	}
	if h.allowsCommand(protocol.Identity{Role: "admin", Access: protocol.Access{AllDevices: true}}, protocol.Command{Kind: "computer.act", DeviceID: h.config.DeviceID}) {
		t.Fatal("input bypassed Hub computer control")
	}
	if err = h.stopRemoteOwner(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	tool.CallID, tool.Arguments.Operation = "late-observe", "observe"
	if _, err = bridge.executeTool(context.Background(), tool); err == nil {
		t.Fatal("late computer operation crossed Hub stop barrier")
	}
	status, err := h.computerStatus(session.ID, h.config.DeviceID)
	if err != nil || status.Session.State != "stopped" {
		t.Fatal(status, err)
	}
}
