package evaluation

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"mewdex/internal/protocol"
)

func sequentialFixture() (Trace, Case) {
	project := protocol.Project{ID: "project", Directories: []protocol.ProjectDirectory{
		{DeviceID: "first-node", ProjectID: "first-root", Path: "/first"},
		{DeviceID: "second-node", ProjectID: "second-root", Path: "/second"},
	}}
	harness := protocol.ProjectHarnessState{Version: "captured", Revision: 3}
	trace := Trace{Session: protocol.Session{ID: "conversation", LastStartID: "start", State: "idle", Model: "model", Policy: protocol.Policy{AccessMode: "read-only"}}, Context: protocol.ProjectTurnContext{SessionID: "conversation", CommandID: "start", Project: project, Harness: harness}}
	test := Case{ID: "steps", Prompt: "Run the steps in order", SequentialCommands: true}
	for index, directory := range project.Directories {
		command := []string{"printf", directory.DeviceID}
		jobID, callID := "job-"+directory.DeviceID, "call-"+directory.DeviceID
		arguments := map[string]any{"directory": index, "command": command}
		call := map[string]any{"id": callID, "type": "dynamicToolCall", "tool": "mewdex_exec", "arguments": arguments, "status": "completed", "success": true, "contentItems": []map[string]string{{"type": "inputText", "text": string(protocol.JSON(map[string]string{"id": jobID, "state": "completed"}))}}}
		job := map[string]any{"id": jobID, "type": "commandExecution", "status": "completed", "commandArgs": command, "exitCode": 0, "aggregatedOutput": "actual", "executionTarget": protocol.ExecutionTarget{DeviceID: directory.DeviceID, ProjectID: directory.ProjectID, Workspace: directory.Path, AccessMode: "read-only", HarnessVersion: harness.Version, HarnessRevision: harness.Revision}}
		trace.Events = append(trace.Events,
			protocol.Event{Sequence: int64(index*3 + 1), SessionID: trace.Session.ID, Kind: "engine.item/started", Payload: protocol.JSON(map[string]any{"item": call})},
			protocol.Event{Sequence: int64(index*3 + 2), SessionID: trace.Session.ID, Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": job})},
			protocol.Event{Sequence: int64(index*3 + 3), SessionID: trace.Session.ID, Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": call})},
		)
		test.Checks = append(test.Checks, Check{Kind: "command", DeviceID: directory.DeviceID, ProjectID: directory.ProjectID, Command: command, Count: intValue(1)})
	}
	return trace, test
}

func changeSequenceItem(t *testing.T, trace *Trace, index int, change func(map[string]any)) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(trace.Events[index].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	change(payload["item"].(map[string]any))
	trace.Events[index].Payload = protocol.JSON(payload)
}

func sequenceGrade(trace Trace, test Case) Grade {
	return GradeTrace(test, Model{ID: "model"}, trace.Context.Project, trace.Context.Harness, trace)
}

func TestSequentialCommandsAcceptConfirmedStepsAndUnorderedStorage(t *testing.T) {
	trace, test := sequentialFixture()
	if grade := sequenceGrade(trace, test); !grade.Passed {
		t.Fatal(grade)
	}
	slices.Reverse(trace.Events)
	if grade := sequenceGrade(trace, test); !grade.Passed {
		t.Fatal("event storage order replaced authoritative sequence numbers", grade)
	}
}

func TestSequentialCommandsRejectPreviouslyUndetectedOrderingAndReceiptFailures(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*testing.T, *Trace)
	}{
		{"reversed steps", func(_ *testing.T, trace *Trace) {
			for index := range trace.Events {
				if index < 3 {
					trace.Events[index].Sequence += 3
				} else {
					trace.Events[index].Sequence -= 3
				}
			}
		}},
		{"overlapping steps", func(_ *testing.T, trace *Trace) {
			for index, sequence := range []int64{1, 4, 5, 2, 6, 7} {
				trace.Events[index].Sequence = sequence
			}
		}},
		{"async job still running", func(t *testing.T, trace *Trace) {
			trace.Events[1].Sequence = 8
			changeSequenceItem(t, trace, 2, func(item map[string]any) {
				item["contentItems"] = []map[string]string{{"type": "inputText", "text": `{"id":"job-first-node","state":"running"}`}}
			})
		}},
		{"missing invocation start", func(_ *testing.T, trace *Trace) { trace.Events = trace.Events[1:] }},
		{"missing invocation completion", func(_ *testing.T, trace *Trace) { trace.Events = append(trace.Events[:2], trace.Events[3:]...) }},
		{"duplicate invocation start", func(_ *testing.T, trace *Trace) { trace.Events = append(trace.Events, trace.Events[0]) }},
		{"missing job receipt", func(t *testing.T, trace *Trace) {
			changeSequenceItem(t, trace, 2, func(item map[string]any) { item["contentItems"] = nil })
		}},
		{"ambiguous job receipts", func(t *testing.T, trace *Trace) {
			changeSequenceItem(t, trace, 2, func(item map[string]any) {
				item["contentItems"] = []map[string]string{{"type": "inputText", "text": `{"id":"job-first-node"}`}, {"type": "inputText", "text": `{"id":"job-second-node"}`}}
			})
		}},
		{"unsuccessful invocation", func(t *testing.T, trace *Trace) {
			changeSequenceItem(t, trace, 2, func(item map[string]any) { item["success"] = false })
		}},
		{"wrong job receipt", func(t *testing.T, trace *Trace) {
			changeSequenceItem(t, trace, 2, func(item map[string]any) {
				item["contentItems"] = []map[string]string{{"type": "inputText", "text": `{"id":"job-second-node"}`}}
			})
		}},
		{"duplicate job receipt", func(t *testing.T, trace *Trace) {
			changeSequenceItem(t, trace, 5, func(item map[string]any) {
				item["contentItems"] = []map[string]string{{"type": "inputText", "text": `{"id":"job-first-node"}`}}
			})
		}},
		{"start arguments changed", func(t *testing.T, trace *Trace) {
			changeSequenceItem(t, trace, 0, func(item map[string]any) {
				item["arguments"].(map[string]any)["command"] = []string{"printf", "different"}
			})
		}},
		{"completion arguments changed", func(t *testing.T, trace *Trace) {
			changeSequenceItem(t, trace, 2, func(item map[string]any) { item["arguments"].(map[string]any)["directory"] = 1 })
		}},
		{"foreign invocation completion", func(_ *testing.T, trace *Trace) { trace.Events[2].SessionID = "another-conversation" }},
		{"result recorded before start", func(_ *testing.T, trace *Trace) {
			trace.Events[0].Sequence, trace.Events[1].Sequence = 2, 1
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			trace, test := sequentialFixture()
			scenario.change(t, &trace)
			legacy := test
			legacy.SequentialCommands = false
			if grade := sequenceGrade(trace, legacy); !grade.Passed {
				t.Fatal("fixture no longer isolates the original coverage gap", grade)
			}
			grade := sequenceGrade(trace, test)
			if grade.Passed || !slices.ContainsFunc(grade.Checks, func(check CheckResult) bool { return check.Name == "command sequence" && !check.Passed }) {
				t.Fatal("sequence failure was accepted", grade)
			}
		})
	}
}

func TestSequentialCommandsWaitForAsyncJobCompletionAndExpandCounts(t *testing.T) {
	t.Run("explicitly expected failed command", func(t *testing.T) {
		trace, test := sequentialFixture()
		test.Checks[0].Status, test.Checks[0].ExitCode = "failed", intValue(2)
		changeSequenceItem(t, &trace, 1, func(item map[string]any) {
			item["status"], item["exitCode"] = "failed", 2
		})
		if grade := sequenceGrade(trace, test); !grade.Passed {
			t.Fatal("explicit failure contract was lost", grade)
		}
	})
	t.Run("async result completes before next start", func(t *testing.T) {
		trace, test := sequentialFixture()
		for index, sequence := range []int64{1, 6, 3, 8, 9, 10} {
			trace.Events[index].Sequence = sequence
		}
		changeSequenceItem(t, &trace, 2, func(item map[string]any) {
			item["contentItems"] = []map[string]string{{"type": "inputText", "text": `{"id":"job-first-node","state":"running"}`}}
		})
		if grade := sequenceGrade(trace, test); !grade.Passed {
			t.Fatal("later confirmed job result was lost", grade)
		}
	})
	t.Run("repeated commands use separate durable jobs", func(t *testing.T) {
		trace, test := sequentialFixture()
		test.Checks = test.Checks[:1]
		test.Checks[0].Count = intValue(2)
		for _, index := range []int{3, 5} {
			changeSequenceItem(t, &trace, index, func(item map[string]any) {
				item["arguments"] = map[string]any{"directory": 0, "command": test.Checks[0].Command}
			})
		}
		changeSequenceItem(t, &trace, 4, func(item map[string]any) {
			item["commandArgs"] = test.Checks[0].Command
			item["executionTarget"] = protocol.ExecutionTarget{DeviceID: "first-node", ProjectID: "first-root", Workspace: "/first", AccessMode: "read-only", HarnessVersion: "captured", HarnessRevision: 3}
		})
		if grade := sequenceGrade(trace, test); !grade.Passed {
			t.Fatal(grade)
		}
	})
}

func TestSequentialContractRequiresCommandsAndPreservesDefaultSuiteEncoding(t *testing.T) {
	suite := evaluationSuite()
	raw := string(protocol.JSON(suite))
	if strings.Contains(raw, "sequentialCommands") {
		t.Fatal("default suite encoding changed", raw)
	}
	suite.Cases[0].SequentialCommands = true
	if suite.Validate() == nil {
		t.Fatal("sequential task without commands was accepted")
	}
}

func TestUnrelatedToolArgumentsKeepTheirExistingEvidenceFormat(t *testing.T) {
	trace, test := sequentialFixture()
	trace.Events = append(trace.Events, protocol.Event{Sequence: 7, SessionID: trace.Session.ID, Kind: "engine.item/completed", Payload: protocol.JSON(map[string]any{"item": map[string]any{
		"id": "another-tool", "type": "dynamicToolCall", "tool": "another-tool", "arguments": "opaque provider arguments", "status": "completed", "success": true,
	}})})
	if grade := sequenceGrade(trace, test); !grade.Passed {
		t.Fatal("another tool's argument schema affected command sequencing", grade)
	}
	test.SequentialCommands = false
	if grade := sequenceGrade(trace, test); !grade.Passed {
		t.Fatal("existing evidence parsing changed", grade)
	}
}
