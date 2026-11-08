package evaluation

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"mewdex/internal/protocol"
)

type Trace struct {
	Session protocol.Session            `json:"session"`
	Context protocol.ProjectTurnContext `json:"context"`
	Events  []protocol.Event            `json:"events"`
}

type CheckResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
}

type Grade struct {
	Passed bool          `json:"passed"`
	Checks []CheckResult `json:"checks"`
}

type traceItem struct {
	ID              string                    `json:"id"`
	Type            string                    `json:"type"`
	Text            string                    `json:"text"`
	Status          string                    `json:"status"`
	Operation       string                    `json:"operation"`
	Command         []string                  `json:"commandArgs"`
	Output          string                    `json:"aggregatedOutput"`
	ExitCode        *int                      `json:"exitCode"`
	Target          *protocol.ExecutionTarget `json:"executionTarget"`
	sequence        int64
	startedSequence int64
	startCount      int
	startedType     string
	raw             json.RawMessage
	startedRaw      json.RawMessage
}

func traceItems(trace Trace) ([]traceItem, error) {
	items := map[string]traceItem{}
	starts := map[string]traceItem{}
	startCounts := map[string]int{}
	for _, event := range trace.Events {
		if event.SessionID != trace.Session.ID || event.Kind != "engine.item/started" && event.Kind != "engine.item/completed" {
			continue
		}
		var payload struct {
			Item json.RawMessage `json:"item"`
		}
		var item traceItem
		if json.Unmarshal(event.Payload, &payload) != nil || json.Unmarshal(payload.Item, &item) != nil || item.ID == "" || item.Type == "" {
			return nil, fmt.Errorf("invalid item evidence at event %d", event.Sequence)
		}
		if item.Type == "dynamicToolCall" {
			item.raw = payload.Item
		}
		item.sequence = event.Sequence
		if event.Kind == "engine.item/started" {
			item.Status = "inProgress"
			startCounts[item.ID]++
			if previous, found := starts[item.ID]; !found || event.Sequence < previous.sequence {
				starts[item.ID] = item
			}
		} else if item.Type == "agentMessage" {
			item.Status = "completed"
		}
		if prior, ok := items[item.ID]; !ok || prior.sequence < item.sequence {
			items[item.ID] = item
		}
	}
	result := make([]traceItem, 0, len(items))
	for _, item := range items {
		start := starts[item.ID]
		item.startedSequence = start.sequence
		item.startedType = start.Type
		item.startedRaw = start.raw
		item.startCount = startCounts[item.ID]
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].sequence < result[j].sequence })
	return result, nil
}

func GradeTrace(test Case, model Model, project protocol.Project, harness protocol.ProjectHarnessState, trace Trace) Grade {
	grade := Grade{Passed: true, Checks: []CheckResult{}}
	add := func(name string, passed bool, expected, observed string) {
		grade.Checks = append(grade.Checks, CheckResult{Name: name, Passed: passed, Expected: expected, Observed: observed})
		grade.Passed = grade.Passed && passed
	}
	add("session", trace.Session.State == "idle" && !trace.Session.Frozen && !trace.Session.ExecutionUnconfirmed(), "idle, confirmed", trace.Session.State+" "+trace.Session.StopStatus)
	add("model", trace.Session.Model == model.ID, model.ID, trace.Session.Model)
	context := trace.Context
	add("configuration", context.SessionID == trace.Session.ID && context.CommandID == trace.Session.LastStartID && context.Harness.Version == harness.Version && context.Harness.Revision == harness.Revision && context.Project.ID == project.ID && slices.Equal(context.Project.Directories, project.Directories), harness.Version, context.Harness.Version)
	items, evidenceErr := traceItems(trace)
	add("item evidence", evidenceErr == nil, "complete item records", fmt.Sprint(evidenceErr))
	confirmed := true
	for _, item := range items {
		if slices.Contains([]string{"commandExecution", "fileChange", "mcpToolCall", "computerUse"}, item.Type) && !slices.Contains([]string{"completed", "failed", "cancelled", "stopped"}, item.Status) {
			confirmed = false
		}
	}
	add("tool outcomes", confirmed, "confirmed terminal outcomes", fmt.Sprint(confirmed))
	parts := []string{}
	for _, item := range items {
		if item.Type == "agentMessage" && item.Status == "completed" {
			parts = append(parts, item.Text)
		}
	}
	answer := strings.Join(parts, "\n")
	access := trace.Session.Policy.AccessMode
	if trace.Session.Policy.CollaborationMode == "plan" {
		access = "read-only"
	}
	if test.SequentialCommands {
		sequenceErr := commandSequence(test.Checks, items, project, harness, access)
		observed := "commands completed in the requested sequence"
		if sequenceErr != nil {
			observed = sequenceErr.Error()
		}
		add("command sequence", sequenceErr == nil, "ordered commands with confirmed completion before each next start", observed)
	}
	for index, check := range test.Checks {
		name := fmt.Sprintf("%d %s", index+1, check.Kind)
		switch check.Kind {
		case "assistant.equals":
			add(name, answer == check.Value, check.Value, answer)
		case "assistant.contains":
			add(name, strings.Contains(answer, check.Value), check.Value, answer)
		case "tool.count":
			count := 0
			for _, item := range items {
				if item.Type == check.ToolType {
					count++
				}
			}
			add(name, count == *check.Count, fmt.Sprintf("%s = %d", check.ToolType, *check.Count), fmt.Sprint(count))
		case "command":
			count := 0
			for _, item := range items {
				if matchesCommand(item, check, project, harness, access) {
					count++
				}
			}
			add(name, count == *check.Count, fmt.Sprintf("%d matching dispatched commands", *check.Count), fmt.Sprint(count))
		case "computer":
			count := 0
			for _, item := range items {
				if item.Type == "computerUse" && item.Operation == check.Operation && item.Status == "completed" && matchesTarget(item.Target, check, project, harness, "window-grant") {
					count++
				}
			}
			add(name, count == *check.Count, fmt.Sprintf("%d completed %s requests", *check.Count, check.Operation), fmt.Sprint(count))
		}
	}
	return grade
}

func matchesCommand(item traceItem, check Check, project protocol.Project, harness protocol.ProjectHarnessState, access string) bool {
	status := check.Status
	if status == "" {
		status = "completed"
	}
	if item.Type != "commandExecution" || !matchesTarget(item.Target, check, project, harness, access) || !slices.Equal(item.Command, check.Command) || item.Status != status || !strings.Contains(item.Output, check.OutputContains) {
		return false
	}
	if check.ExitCode != nil && (item.ExitCode == nil || *item.ExitCode != *check.ExitCode) || status == "completed" && (item.ExitCode == nil || *item.ExitCode != 0) {
		return false
	}
	return true
}

func matchesTarget(target *protocol.ExecutionTarget, check Check, project protocol.Project, harness protocol.ProjectHarnessState, access string) bool {
	if target == nil || target.DeviceID != check.DeviceID || target.ProjectID != check.ProjectID || target.AccessMode != access || target.HarnessVersion != harness.Version || target.HarnessRevision != harness.Revision {
		return false
	}
	return slices.ContainsFunc(project.Directories, func(directory protocol.ProjectDirectory) bool {
		return directory.DeviceID == target.DeviceID && directory.ProjectID == target.ProjectID && directory.Path == target.Workspace
	})
}
