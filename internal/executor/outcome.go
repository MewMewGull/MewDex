package executor

import (
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func commandFailure(err error) protocol.CommandResult {
	state := "failed"
	if engine.IsUnknownOutcome(err) {
		state = "unknown"
	}
	return protocol.CommandResult{State: state, Error: err.Error()}
}
