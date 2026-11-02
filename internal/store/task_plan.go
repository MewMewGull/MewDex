package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"mewdex/internal/protocol"
)

// TaskPlan reads the latest reported plan at the same exclusive cursor as
// history paging. A new turn without a plan never inherits the preceding plan.
func (s *Store) TaskPlan(ctx context.Context, session protocol.Session, before int64) (*protocol.TaskPlan, error) {
	if before == 0 {
		before = math.MaxInt64
	}
	var sequence int64
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT sequence,data FROM events WHERE session=? AND device=? AND sequence<?
		AND json_extract(data,'$.kind')='engine.turn/plan/updated' ORDER BY sequence DESC LIMIT 1`, session.ID, session.DeviceID, before).Scan(&sequence, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var event protocol.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, err
	}
	var payload struct {
		TurnID      string                  `json:"turnId"`
		Explanation string                  `json:"explanation"`
		Plan        []protocol.TaskPlanStep `json:"plan"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return nil, err
	}
	if payload.TurnID == "" {
		return nil, nil
	}
	view := &protocol.TaskPlan{DeviceID: session.DeviceID, SessionID: session.ID, TurnID: payload.TurnID, Sequence: sequence, UpdatedAt: event.CreatedAt, Explanation: payload.Explanation, Steps: payload.Plan}
	if view.Steps == nil {
		view.Steps = []protocol.TaskPlanStep{}
	}
	var lifecycleRaw []byte
	var lifecycleSequence int64
	err = s.db.QueryRowContext(ctx, `SELECT sequence,data FROM events WHERE session=? AND device=? AND sequence<?
		AND json_extract(data,'$.kind') IN ('engine.turn/started','engine.turn/completed') ORDER BY sequence DESC LIMIT 1`, session.ID, session.DeviceID, before).Scan(&lifecycleSequence, &lifecycleRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return nil, err
	}
	var lifecycle protocol.Event
	if err := json.Unmarshal(lifecycleRaw, &lifecycle); err != nil {
		return nil, err
	}
	var state struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(lifecycle.Payload, &state); err != nil {
		return nil, err
	}
	if state.Turn.ID != view.TurnID || lifecycle.Kind == "engine.turn/started" && lifecycleSequence > sequence {
		return nil, nil
	}
	view.TurnStatus = state.Turn.Status
	return view, nil
}
