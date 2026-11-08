package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"mewdex/internal/protocol"
)

func (s *Store) ComputerSession() (protocol.ComputerSession, error) {
	var current string
	if err := s.Get("computer_control", "current", &current); err != nil {
		return protocol.ComputerSession{}, err
	}
	var value protocol.ComputerSession
	err := s.Get("computer_sessions", current, &value)
	return value, err
}

// SaveComputerSession atomically compares the current revision and keeps every
// session record. A stopped session's unknown action outcome remains visible.
func (s *Store) SaveComputerSession(value protocol.ComputerSession, expected int64) (protocol.ComputerSession, error) {
	if value.ID == "" || value.OwnerID == "" || value.ProjectID == "" || value.DeviceID == "" || value.InstanceID == "" {
		return value, errors.New("computer session identity required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	var raw []byte
	var currentID string
	var old protocol.ComputerSession
	err = tx.QueryRow("SELECT data FROM objects WHERE bucket='computer_control' AND id='current'").Scan(&raw)
	if err == nil {
		if err = json.Unmarshal(raw, &currentID); err != nil {
			return value, err
		}
		if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='computer_sessions' AND id=?", currentID).Scan(&raw); err != nil {
			return value, err
		}
		if err = json.Unmarshal(raw, &old); err != nil {
			return value, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return value, err
	}
	if old.Revision != expected {
		return value, ErrConflict
	}
	if old.ID != "" && old.ID != value.ID && old.State != "stopped" && old.State != "expired" {
		return value, errors.New("computer control is already reserved")
	}
	if old.ID != value.ID {
		var existing int
		err = tx.QueryRow("SELECT 1 FROM objects WHERE bucket='computer_sessions' AND id=?", value.ID).Scan(&existing)
		if err == nil {
			return value, ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return value, err
		}
	}
	if old.ID == value.ID && (old.OwnerID != value.OwnerID || old.ProjectID != value.ProjectID || old.DeviceID != value.DeviceID || old.InstanceID != value.InstanceID || old.CreatedAt != value.CreatedAt) {
		return value, errors.New("computer session ownership is immutable")
	}
	if old.ID == value.ID && (old.State == "stopped" || old.State == "expired") && value.State != old.State {
		return value, errors.New("a finished computer session requires a new local grant")
	}
	value.Revision, value.UpdatedAt = expected+1, protocol.Now()
	if _, err = tx.Exec("INSERT INTO objects VALUES('computer_sessions',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", value.ID, protocol.JSON(value)); err != nil {
		return value, err
	}
	if _, err = tx.Exec("INSERT INTO objects VALUES('computer_control','current',?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", protocol.JSON(value.ID)); err != nil {
		return value, err
	}
	event := protocol.Event{ID: protocol.ID(), DeviceID: value.DeviceID, Kind: "computer.session.updated", Payload: protocol.JSON(value), CreatedAt: value.UpdatedAt}
	if _, err = tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, value.DeviceID, "", protocol.JSON(event)); err != nil {
		return value, err
	}
	return value, tx.Commit()
}
