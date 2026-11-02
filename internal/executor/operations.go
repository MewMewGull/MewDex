package executor

import (
	"context"
	"errors"

	"mewdex/internal/protocol"
)

// Ordinary session RPC waits can be cancelled without replaying their writes.
// Stop owns a separate path, so it need not wait for an ordinary RPC's timeout.
func (x *Executor) operation(ctx context.Context, session string) (context.Context, func(), error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closing || x.stopRequests[session] > 0 {
		return ctx, func() {}, errors.New("a stop is pending; refresh after it completes")
	}
	ctx, cancel := context.WithCancel(ctx)
	x.operations[session] = cancel
	return ctx, func() {
		cancel()
		x.mu.Lock()
		delete(x.operations, session)
		x.mu.Unlock()
	}, nil
}

func (x *Executor) prioritizeStop(c protocol.Command) func() {
	var session protocol.Session
	if x.store.Get("sessions", c.SessionID, &session) != nil || session.DeviceID != c.DeviceID || (c.ExpectedTurnID != "" && c.ExpectedTurnID != session.TurnID) {
		return func() {}
	}
	x.mu.Lock()
	x.stopRequests[c.SessionID]++
	cancel := x.operations[c.SessionID]
	x.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return func() {
		x.mu.Lock()
		x.stopRequests[c.SessionID]--
		if x.stopRequests[c.SessionID] == 0 {
			delete(x.stopRequests, c.SessionID)
		}
		x.mu.Unlock()
	}
}
