package executor

import (
	"context"
	"encoding/json"
	"testing"

	"mewdex/internal/protocol"
)

func TestTerminalObservationDoesNotMutateWorkerOwnedDirectorySlices(t *testing.T) {
	x, database, _ := setup(t)
	x.terminal = &terminalState{runs: map[string]*terminalRun{}}
	defer func() { x.terminal = nil }()
	job := protocol.TerminalJob{ID: "job", WorkspaceIDs: []string{"worker-owned"}, State: "running"}
	saved := job
	saved.WorkspaceIDs = []string{"stored-snapshot"}
	if err := database.Put("terminal_jobs", job.ID, saved); err != nil {
		t.Fatal(err)
	}
	result := x.waitTerminal(context.Background(), job, 0)
	var observed protocol.TerminalJob
	if result.State != "completed" || json.Unmarshal(result.Data, &observed) != nil || observed.WorkspaceIDs[0] != "stored-snapshot" {
		t.Fatal(result)
	}
	if job.WorkspaceIDs[0] != "worker-owned" {
		t.Fatal("observation decoded into the background worker's slice")
	}
}
