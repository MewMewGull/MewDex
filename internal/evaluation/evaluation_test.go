package evaluation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func intValue(value int) *int { return &value }

func evaluationSuite() Suite {
	return Suite{Format: 1, Name: "Owned task", ProjectID: "project", Models: []Model{{ID: "fixture-model"}}, Cases: []Case{{ID: "reply", Prompt: "Reply with literal", Checks: []Check{{Kind: "assistant.equals", Value: "literal"}, {Kind: "tool.count", ToolType: "commandExecution", Count: intValue(0)}}}}}
}

func TestSuiteRejectsUnboundedAmbiguousAndIncompleteChecks(t *testing.T) {
	valid := evaluationSuite()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Suite){
		func(s *Suite) { s.Models = nil },
		func(s *Suite) { s.Models = append(s.Models, s.Models[0]) },
		func(s *Suite) { s.Cases[0].ID = "../outside" },
		func(s *Suite) { s.Cases[0].Policy.Model = "ambiguous-model" },
		func(s *Suite) { s.Cases[0].Checks = []Check{{Kind: "tool.count", ToolType: "commandExecution"}} },
		func(s *Suite) { s.Cases[0].Checks = []Check{{Kind: "command", Count: intValue(1)}} },
	} {
		suite := evaluationSuite()
		change(&suite)
		if suite.Validate() == nil {
			t.Fatal("invalid suite accepted", suite)
		}
	}
}

func TestGradesUseCompletedScopedEvidenceAndExactArguments(t *testing.T) {
	project := protocol.Project{ID: "project", Directories: []protocol.ProjectDirectory{{DeviceID: "node", ProjectID: "root", Path: "/owned"}}}
	harness := protocol.ProjectHarnessState{Version: "captured", Revision: 3}
	trace := Trace{Session: protocol.Session{ID: "conversation", LastStartID: "start", State: "idle", Model: "model", Policy: protocol.Policy{AccessMode: "read-only"}}, Context: protocol.ProjectTurnContext{SessionID: "conversation", CommandID: "start", Project: project, Harness: harness}}
	item := map[string]any{"id": "job", "type": "commandExecution", "status": "completed", "commandArgs": []string{"printf", "a b"}, "exitCode": 0, "aggregatedOutput": "actual-result", "executionTarget": protocol.ExecutionTarget{DeviceID: "node", ProjectID: "root", Workspace: "/owned", AccessMode: "read-only", HarnessVersion: "captured", HarnessRevision: 3}}
	trace.Events = []protocol.Event{
		{Sequence: 1, SessionID: "conversation", Kind: "engine.item/started", Payload: protocol.JSON(map[string]any{"item": item})},
		{Sequence: 2, SessionID: "conversation", Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": item})},
		{Sequence: 3, SessionID: "other", Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": map[string]string{"id": "foreign", "type": "agentMessage", "text": "wrong"}})},
		{Sequence: 4, SessionID: "conversation", Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": map[string]string{"id": "answer", "type": "agentMessage", "text": "literal"}})},
	}
	test := Case{Checks: []Check{{Kind: "assistant.equals", Value: "literal"}, {Kind: "command", DeviceID: "node", ProjectID: "root", Command: []string{"printf", "a b"}, OutputContains: "actual-result", Count: intValue(1)}, {Kind: "tool.count", ToolType: "commandExecution", Count: intValue(1)}}}
	grade := GradeTrace(test, Model{ID: "model"}, project, harness, trace)
	if !grade.Passed {
		t.Fatal(grade)
	}
	test.Checks[1].Command = []string{"printf", "a", "b"}
	if GradeTrace(test, Model{ID: "model"}, project, harness, trace).Passed {
		t.Fatal("shell display text stood in for exact dispatched arguments")
	}
	test.Checks[1].Command = []string{"printf", "a b"}
	item["status"] = "unknown"
	trace.Events = append(trace.Events, protocol.Event{Sequence: 5, SessionID: "conversation", Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": item})})
	if GradeTrace(test, Model{ID: "model"}, project, harness, trace).Passed {
		t.Fatal("unknown tool outcome passed")
	}
	trace.Context.Harness.Version = "newer-configuration"
	if GradeTrace(test, Model{ID: "model"}, project, harness, trace).Passed {
		t.Fatal("configuration mutation passed")
	}
}

type evaluationFixture struct {
	mu           sync.Mutex
	commands     map[string]protocol.CommandResult
	posts        map[string]int
	session      protocol.Session
	project      protocol.Project
	harness      protocol.ProjectHarnessState
	lostStart    bool
	missingStart bool
}

func (f *evaluationFixture) serve(w http.ResponseWriter, request *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if request.Header.Get("Authorization") != "Bearer fixture-secret" {
		w.WriteHeader(401)
		return
	}
	var value any
	switch {
	case request.URL.Path == "/health":
		value = protocol.ServerInfo{Name: "MewDex", HubDeviceID: "hub", ProtocolVersion: protocol.Version}
	case request.URL.Path == "/v1/me":
		value = protocol.Identity{ID: "input", Role: "client"}
	case request.URL.Path == "/v1/devices":
		value = []protocol.Device{{ID: "coordinator", Roles: []string{"coordinator"}, Projects: []protocol.Project{f.project}}}
	case request.URL.Path == "/v1/projects/project/harness":
		value = f.harness
	case request.URL.Path == "/v1/projects/project/harness/turn":
		value = protocol.ProjectTurnContext{SessionID: f.session.ID, CommandID: f.session.LastStartID, Project: f.project, Harness: f.harness}
	case request.URL.Path == "/v1/commands" && request.Method == "POST":
		var command protocol.Command
		if json.NewDecoder(request.Body).Decode(&command) != nil {
			w.WriteHeader(400)
			return
		}
		f.posts[command.Kind]++
		result := protocol.CommandResult{CommandID: command.ID, State: "completed"}
		switch command.Kind {
		case "model.list":
			result.Data = protocol.JSON(protocol.ModelPage{Runtime: &protocol.ModelRuntime{Adapter: "fixture", Version: "1", Transport: "jsonrpc-stdio"}})
		case "create":
			f.session = protocol.Session{ID: "conversation", ProjectID: "project", DeviceID: "coordinator", State: "idle", Model: command.Policy.Model, ModelProvider: "fixture-provider", Policy: *command.Policy, PolicyVersion: "policy"}
			copy := f.session
			result.Session = &copy
		case "start":
			f.session.LastStartID = command.ID
		}
		if command.Kind != "start" || !f.missingStart {
			f.commands[command.ID] = result
		}
		if command.Kind == "start" && f.lostStart {
			connection, _, _ := w.(http.Hijacker).Hijack()
			connection.Close()
			return
		}
		value = result
	case strings.HasPrefix(request.URL.Path, "/v1/commands/"):
		result, found := f.commands[strings.TrimPrefix(request.URL.Path, "/v1/commands/")]
		if !found {
			w.WriteHeader(404)
			return
		}
		value = result
	case request.URL.Path == "/v1/sessions":
		value = []protocol.Session{f.session}
	case strings.HasPrefix(request.URL.Path, "/v1/sessions/") && request.Method == "PATCH":
		f.session.Archived = true
		value = f.session
	case request.URL.Path == "/v1/events":
		after, _ := strconv.ParseInt(request.URL.Query().Get("after"), 10, 64)
		events := []protocol.Event{}
		for sequence := after + 1; sequence <= 501 && len(events) < 500; sequence++ {
			event := protocol.Event{Sequence: sequence, SessionID: f.session.ID, Kind: "input.received"}
			if sequence == 501 {
				event.Kind, event.Payload = "engine.item/completed", protocol.JSON(map[string]any{"item": map[string]string{"id": "answer", "type": "agentMessage", "text": "literal"}})
			}
			events = append(events, event)
		}
		value = events
	default:
		w.WriteHeader(404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func runnerFixture(t *testing.T) (*evaluationFixture, Connection) {
	t.Helper()
	f := &evaluationFixture{commands: map[string]protocol.CommandResult{}, posts: map[string]int{}, project: protocol.Project{ID: "project"}, harness: protocol.ProjectHarnessState{ProjectID: "project", Version: "fixture-version", Revision: 1}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	connection := Connection{BaseURL: server.URL, HubDeviceID: "hub"}
	connection.Credentials.Token = "fixture-secret"
	return f, connection
}

func TestEvaluationResumesOriginalReceiptsAndKeepsPrivateCredentialsOutOfArtifacts(t *testing.T) {
	f, connection := runnerFixture(t)
	output := t.TempDir()
	suite := evaluationSuite()
	report, err := Run(context.Background(), connection, suite, output, time.Second)
	if err != nil || !report.Passed || !report.Complete || !report.Cases[0].Archived {
		t.Fatal(report, err)
	}
	var trace Trace
	raw, _ := os.ReadFile(filepath.Join(output, report.Cases[0].Trace))
	if json.Unmarshal(raw, &trace) != nil || len(trace.Events) != 501 || trace.Session.ModelProvider != "fixture-provider" {
		t.Fatal("complete scoped trace or provider was lost")
	}
	for _, name := range []string{"report.json", "review.md", report.Cases[0].Trace} {
		raw, err := os.ReadFile(filepath.Join(output, name))
		if err != nil || strings.Contains(string(raw), "fixture-secret") {
			t.Fatal(name, err)
		}
	}
	replayed, err := Run(context.Background(), connection, suite, output, time.Second)
	if err != nil || !replayed.Passed || replayed.RunID != report.RunID {
		t.Fatal(replayed, err)
	}
	f.mu.Lock()
	counts := []int{f.posts["create"], f.posts["start"], f.posts["model.list"]}
	f.mu.Unlock()
	if !slices.Equal(counts, []int{1, 1, 1}) {
		t.Fatal("resumption sent another model input", counts)
	}
	before, _ := os.ReadFile(filepath.Join(output, "report.json"))
	suite.Name = "different-suite"
	if _, err = Run(context.Background(), connection, suite, output, time.Second); err == nil {
		t.Fatal("another suite reused this run")
	}
	after, _ := os.ReadFile(filepath.Join(output, "report.json"))
	if string(before) != string(after) {
		t.Fatal("rejected resume replaced the original report")
	}
}

func TestLostStartResponseUsesOriginalReceiptWithoutResubmitting(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(strconv.FormatBool(missing), func(t *testing.T) {
			f, connection := runnerFixture(t)
			f.mu.Lock()
			f.lostStart, f.missingStart = true, missing
			f.mu.Unlock()
			output := t.TempDir()
			if _, err := Run(context.Background(), connection, evaluationSuite(), output, time.Second); err == nil {
				t.Fatal("lost submission response was assumed complete")
			}
			report, err := Run(context.Background(), connection, evaluationSuite(), output, time.Second)
			if missing && err == nil || !missing && (err != nil || !report.Passed) {
				t.Fatal(missing, report, err)
			}
			f.mu.Lock()
			count := f.posts["start"]
			f.mu.Unlock()
			if count != 1 {
				t.Fatal("unknown input was automatically resubmitted", count)
			}
		})
	}
}

func TestHubClientDoesNotForwardInputCredentialsThroughRedirects(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	connection := Connection{BaseURL: redirect.URL}
	connection.Credentials.Token = "private"
	client, err := newClient(connection)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if client.request(context.Background(), "GET", "/health", nil, &result) == nil || called {
		t.Fatal("redirect received input credentials")
	}
}
