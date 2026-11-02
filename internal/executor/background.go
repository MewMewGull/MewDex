package executor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"mewdex/internal/protocol"
)

func (x *Executor) backgroundRunning(ctx context.Context, thread string) (bool, error) {
	var response struct {
		Data       []json.RawMessage `json:"data"`
		NextCursor *string           `json:"nextCursor"`
	}
	if err := x.engine.Call(ctx, "thread/backgroundTerminals/list", map[string]any{"threadId": thread, "limit": 1}, &response); err != nil {
		return false, err
	}
	if response.Data == nil {
		return false, errors.New("engine did not return a valid background process list")
	}
	return len(response.Data) > 0 || response.NextCursor != nil, nil
}

// The caller holds the session lock. A completed model turn does not prove that
// a background build has exited; do not let another conversation reuse its root.
func (x *Executor) completedWorkspace(s *protocol.Session) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx, finish, err := x.operation(ctx, s.ID)
	if err == nil {
		defer finish()
		var running bool
		running, err = x.backgroundRunning(ctx, s.EngineID)
		if err == nil {
			if running {
				s.StopStatus = "background-running"
				x.watchBackground(s.ID)
			} else {
				x.releaseProject(s)
			}
			return
		}
	}
	s.StopStatus, s.Frozen, s.State = "processes-unchecked", true, "paused"
}

func (x *Executor) watchBackground(session string) {
	x.mu.Lock()
	if x.closing || x.backgroundWatches[session] {
		x.mu.Unlock()
		return
	}
	x.backgroundWatches[session] = true
	x.active.Add(1)
	x.mu.Unlock()
	go func() {
		defer x.active.Done()
		defer func() { x.mu.Lock(); delete(x.backgroundWatches, session); x.mu.Unlock() }()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-x.done:
				return
			case <-ticker.C:
				if !x.refreshBackground(session) {
					return
				}
			}
		}
	}()
}

func (x *Executor) refreshBackground(session string) bool {
	l := x.lock(session)
	l.Lock()
	defer l.Unlock()
	var s protocol.Session
	if x.store.Get("sessions", session, &s) != nil || s.Frozen || s.StopStatus != "background-running" || (s.State != "idle" && s.State != "failed") {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx, finish, err := x.operation(ctx, session)
	if err != nil {
		return false
	}
	defer finish()
	running, err := x.backgroundRunning(ctx, s.EngineID)
	if err != nil {
		s.State, s.Frozen, s.StopStatus = "paused", true, "processes-unchecked"
		_ = x.save(&s)
		return false
	}
	if running {
		return true
	}
	s.StopStatus = ""
	if x.save(&s) != nil {
		return true
	}
	x.releaseProject(&s)
	go x.advance(session)
	return false
}
