package evaluation

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"mewdex/internal/protocol"
)

type commandCall struct {
	traceItem `json:"-"`
	Tool      string `json:"tool"`
	Success   bool   `json:"success"`
	Arguments struct {
		Directory *int     `json:"directory"`
		Command   []string `json:"command"`
	} `json:"arguments"`
	ContentItems []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"contentItems"`
}

// Command checks define the expected invocation sequence. Their counts expand
// repeated steps. A response's durable job ID links each call to its final
// execution record, including jobs that required a later mewdex_wait.
func commandSequence(checks []Check, items []traceItem, project protocol.Project, harness protocol.ProjectHarnessState, access string) error {
	expected := []Check{}
	for _, check := range checks {
		if check.Kind != "command" {
			continue
		}
		if check.Count == nil || *check.Count < 1 || *check.Count > 100 {
			return errors.New("sequential command checks require a bounded positive count")
		}
		for index := 0; index < *check.Count; index++ {
			expected = append(expected, check)
		}
	}
	if len(expected) == 0 {
		return errors.New("sequential execution requires command checks")
	}
	calls := []commandCall{}
	jobs := map[string]traceItem{}
	for _, item := range items {
		if item.Type == "dynamicToolCall" {
			var header struct {
				Tool string `json:"tool"`
			}
			if json.Unmarshal(item.raw, &header) != nil {
				return errors.New("dynamic tool identity cannot be read from the saved evidence")
			}
			if header.Tool == "mewdex_exec" {
				var call commandCall
				if json.Unmarshal(item.raw, &call) != nil {
					return errors.New("command invocation cannot be read from the saved evidence")
				}
				call.traceItem = item
				calls = append(calls, call)
			}
		}
		if item.Type == "commandExecution" {
			jobs[item.ID] = item
		}
	}
	if len(calls) != len(expected) || len(jobs) != len(expected) {
		return fmt.Errorf("expected %d command calls and jobs, observed %d calls and %d jobs", len(expected), len(calls), len(jobs))
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].startedSequence < calls[j].startedSequence })
	used := map[string]bool{}
	var previousEnd int64
	for index, call := range calls {
		if call.startCount != 1 || call.startedSequence <= 0 || call.sequence <= call.startedSequence || call.Status != "completed" || !call.Success {
			return fmt.Errorf("command %d has incomplete or ambiguous invocation evidence", index+1)
		}
		if call.startedSequence <= previousEnd {
			return fmt.Errorf("command %d started before the previous command was confirmed complete", index+1)
		}
		directoryIndex := call.Arguments.Directory
		if directoryIndex == nil || *directoryIndex < 0 || *directoryIndex >= len(project.Directories) {
			return fmt.Errorf("command %d has no valid captured directory", index+1)
		}
		directory := project.Directories[*directoryIndex]
		check := expected[index]
		var start commandCall
		if json.Unmarshal(call.startedRaw, &start) != nil || call.startedType != call.Type || start.Tool != call.Tool || start.Arguments.Directory == nil || *start.Arguments.Directory != *directoryIndex || !slices.Equal(start.Arguments.Command, call.Arguments.Command) {
			return fmt.Errorf("command %d invocation arguments changed in the saved evidence", index+1)
		}
		if directory.DeviceID != check.DeviceID || directory.ProjectID != check.ProjectID || !slices.Equal(call.Arguments.Command, check.Command) {
			return fmt.Errorf("command %d differs from the requested sequence", index+1)
		}
		jobID := ""
		for _, content := range call.ContentItems {
			if content.Type != "inputText" {
				continue
			}
			var response struct {
				ID string `json:"id"`
			}
			if json.Unmarshal([]byte(content.Text), &response) != nil || response.ID == "" || jobID != "" {
				return fmt.Errorf("command %d has an ambiguous job receipt", index+1)
			}
			jobID = response.ID
		}
		job, found := jobs[jobID]
		if !found || used[jobID] || job.sequence <= call.startedSequence || !matchesCommand(job, check, project, harness, access) || job.Target.Workspace != directory.Path {
			return fmt.Errorf("command %d has no matching confirmed execution result", index+1)
		}
		used[jobID] = true
		previousEnd = max(call.sequence, job.sequence)
	}
	return nil
}
