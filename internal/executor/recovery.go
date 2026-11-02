package executor

import (
	"errors"

	"mewdex/internal/protocol"
)

func recoveryNeeded(s protocol.Session) bool {
	return s.ExecutionUnconfirmed()
}

func (x *Executor) decorateRecovery(s *protocol.Session) error {
	view, err := x.store.ExecutionRecovery(*s)
	if err != nil {
		return err
	}
	if view != nil && view.State != "retired" && !recoveryNeeded(*s) {
		view = nil
	}
	s.ExecutionRecovery = view
	return nil
}

func (x *Executor) retireExecution(s *protocol.Session, c protocol.Command) error {
	if s.ExecutionRecovery == nil || s.ExecutionRecovery.ID != c.RequestID || c.RequestID == "" {
		return errors.New("this execution has no matching verified restart recovery review")
	}
	if s.ExecutionRecovery.State != "retired" && !s.Frozen {
		return errors.New("freeze this execution before ending its unknown outcome")
	}
	updated, event, err := x.store.RetireExecution(*s, c)
	if err != nil {
		return err
	}
	*s = updated
	// The durable retirement is authoritative before releasing the in-memory
	// claim. It cannot reopen a project after an interrupted control response.
	x.releaseProject(s)
	if event.ID != "" {
		x.publish(event)
	}
	return nil
}
