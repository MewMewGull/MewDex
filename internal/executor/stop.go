package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"mewdex/internal/protocol"
)

type observedTurn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Items  []struct {
		Type     string `json:"type"`
		ClientID string `json:"clientId"`
	} `json:"items"`
}

type executionView = protocol.ExecutionObservation

func (x *Executor) readExecution(ctx context.Context, s protocol.Session) (executionView, error) {
	var metadata struct {
		Thread struct {
			ID     string `json:"id"`
			Status struct {
				Type string `json:"type"`
			} `json:"status"`
		} `json:"thread"`
	}
	if err := x.engine.Call(ctx, "thread/read", map[string]any{"threadId": s.EngineID, "includeTurns": false}, &metadata); err != nil {
		return executionView{}, err
	}
	view := executionView{EngineState: metadata.Thread.Status.Type}
	if metadata.Thread.ID != s.EngineID || (view.EngineState != "active" && view.EngineState != "idle" && view.EngineState != "systemError") {
		return view, fmt.Errorf("engine execution status could not be confirmed (%s)", view.EngineState)
	}
	cursor := ""
	for page := 0; page < 10; page++ {
		var turns struct {
			Data       []observedTurn `json:"data"`
			NextCursor *string        `json:"nextCursor"`
		}
		params := map[string]any{"threadId": s.EngineID, "limit": 50, "sortDirection": "desc", "itemsView": "summary"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := x.engine.Call(ctx, "thread/turns/list", params, &turns); err != nil {
			return view, err
		}
		for _, turn := range turns.Data {
			clientID := ""
			for _, item := range turn.Items {
				if item.Type == "userMessage" && item.ClientID == s.LastStartID && s.LastStartID != "" {
					clientID = item.ClientID
				}
			}
			if (s.TurnID != "" && turn.ID == s.TurnID) || clientID != "" {
				view.TurnID, view.TurnStatus, view.ClientID, view.Matched = turn.ID, turn.Status, clientID, true
				return view, nil
			}
		}
		if turns.NextCursor == nil || *turns.NextCursor == "" {
			break
		}
		if *turns.NextCursor == cursor {
			return view, errors.New("engine returned a repeated turn cursor")
		}
		cursor = *turns.NextCursor
	}
	if s.LastStartID != "" || s.TurnID != "" || s.State == "unknown" || s.State == "running" || s.State == "stopping" {
		return view, errors.New("original turn is not identifiable in engine history; execution remains unconfirmed")
	}
	return view, nil
}

func executionQuiescent(view executionView) bool {
	if view.EngineState != "idle" && view.EngineState != "systemError" {
		return false
	}
	if !view.Matched {
		return view.EngineState == "idle"
	}
	return view.TurnStatus == "completed" || view.TurnStatus == "failed" || view.TurnStatus == "interrupted"
}

func (x *Executor) updateObservedQueue(s protocol.Session, view executionView) error {
	items, err := x.store.QueueItems(s.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.State != "unknown" && item.State != "running" && item.State != "dispatching" {
			continue
		}
		if !view.Matched || (item.TurnID != view.TurnID && (view.ClientID == "" || item.ID != view.ClientID)) {
			return errors.New("queued dispatch is not bound to the observed turn")
		}
		state := view.TurnStatus
		if state == "inProgress" {
			state = "running"
		}
		if state != "running" && state != "completed" && state != "failed" && state != "interrupted" {
			return errors.New("engine did not return a recognized turn status")
		}
		if item.State == state && item.TurnID == view.TurnID {
			continue
		}
		item.State, item.TurnID, item.Error = state, view.TurnID, ""
		item.Revision++
		item.UpdatedAt = protocol.Now()
		if err = x.recordQueue(&item); err != nil {
			return err
		}
	}
	return nil
}

func (x *Executor) stop(ctx context.Context, s *protocol.Session, kind string) (err error) {
	s.Frozen = true
	defer func() {
		if err != nil {
			s.State, s.StopStatus = "unknown", "unconfirmed"
			_ = x.save(s)
		}
	}()
	if err = x.expireInteractions(s.ID, "", nil); err != nil {
		return err
	}
	if !s.EngineStarted {
		items, e := x.store.QueueItems(s.ID)
		if e != nil {
			return e
		}
		for _, item := range items {
			if item.State == "dispatching" || item.State == "unknown" {
				item.State, item.Error = "failed", "stopped before a model turn was submitted"
				item.UpdatedAt, item.Revision = protocol.Now(), item.Revision+1
				if e = x.recordQueue(&item); e != nil {
					return e
				}
			}
		}
		s.State, s.StopStatus = "paused", "not-started"
		x.releaseProject(s)
		return x.save(s)
	}
	if err = x.save(s); err != nil {
		return err
	}
	if err = x.load(ctx, s); err != nil {
		return err
	}
	view, err := x.readExecution(ctx, *s)
	if err != nil {
		return err
	}
	if view.EngineState == "active" {
		if !view.Matched || view.TurnID == "" || view.TurnStatus != "inProgress" {
			return errors.New("engine has an active turn that cannot be identified")
		}
		s.TurnID, s.State, s.StopStatus = view.TurnID, "stopping", "pending"
		if err = x.save(s); err != nil {
			return err
		}
		var data json.RawMessage
		interruptErr := x.engine.Call(ctx, "turn/interrupt", map[string]string{"threadId": s.EngineID, "turnId": s.TurnID}, &data)
		// A turn may finish between the read and the interrupt. Observe the
		// original turn instead of treating that race as an automatic replay.
		view, err = x.readExecution(ctx, *s)
		if err != nil {
			return err
		}
		if interruptErr != nil && !executionQuiescent(view) {
			return fmt.Errorf("interruption not confirmed: %w", interruptErr)
		}
	}
	if kind == "stop" {
		var data json.RawMessage
		if err = x.engine.Call(ctx, "thread/backgroundTerminals/clean", map[string]string{"threadId": s.EngineID}, &data); err != nil {
			return err
		}
		running, e := x.backgroundRunning(ctx, s.EngineID)
		if e != nil {
			err = e
			return err
		}
		if running {
			return errors.New("background processes remain; stop is not confirmed")
		}
		s.StopStatus = "registered-processes-cleared"
	} else {
		s.StopStatus = "turn-only"
	}
	if err = x.updateObservedQueue(*s, view); err != nil {
		return err
	}
	if view.EngineState == "active" {
		s.State, s.TurnID = "stopping", view.TurnID
	} else if executionQuiescent(view) {
		s.State, s.TurnID = "paused", ""
		if kind == "stop" {
			x.releaseProject(s)
		}
	} else if view.Matched && view.TurnStatus == "inProgress" {
		s.State, s.TurnID = "stopping", view.TurnID
	} else {
		return errors.New("original turn is not finalized")
	}
	if err = x.save(s); err != nil {
		return err
	}
	if view.Matched && view.ClientID != "" {
		return x.resolveStartReceipt(*s, view)
	}
	return nil
}

func (x *Executor) reconcile(ctx context.Context, s *protocol.Session) (executionView, error) {
	if !s.EngineStarted {
		return executionView{EngineState: "not-started"}, nil
	}
	if err := x.load(ctx, s); err != nil {
		return executionView{}, err
	}
	view, err := x.readExecution(ctx, *s)
	if err != nil {
		return view, err
	}
	if view.EngineState == "active" && view.Matched && view.TurnStatus == "inProgress" {
		s.State, s.TurnID = "running", view.TurnID
	} else if executionQuiescent(view) {
		s.State, s.TurnID, s.Frozen = "paused", "", true
		if s.StopStatus != "registered-processes-cleared" && s.StopStatus != "turn-only" {
			s.StopStatus = "processes-unchecked"
		}
	} else {
		return view, errors.New("original turn is not finalized")
	}
	if err = x.updateObservedQueue(*s, view); err != nil {
		return view, err
	}
	if err = x.save(s); err != nil {
		return view, err
	}
	if view.Matched && view.ClientID != "" {
		if err = x.resolveStartReceipt(*s, view); err != nil {
			return view, err
		}
	}
	return view, nil
}

func (x *Executor) resolveStartReceipt(s protocol.Session, view executionView) error {
	c, err := x.store.Command(view.ClientID)
	if err != nil || c.Kind != "start" || c.SessionID != s.ID || c.DeviceID != s.DeviceID {
		return nil
	}
	r, err := x.store.Result(c.ID)
	if err != nil || (r.State != "unknown" && r.State != "accepted") {
		return err
	}
	r = protocol.CommandResult{CommandID: c.ID, State: "completed", Session: &s, Data: protocol.JSON(view)}
	if err = x.store.Finish(r); err != nil {
		return err
	}
	return x.emit(s.ID, "command.result", r)
}
