package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"

	"mewdex/internal/protocol"
)

func (s *Store) RecordOutput(output protocol.OutputFile) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	raw := protocol.JSON(output)
	var previous []byte
	err = tx.QueryRow("SELECT data FROM objects WHERE bucket='outputs' AND id=?", output.ID).Scan(&previous)
	if err == nil {
		if !bytes.Equal(previous, raw) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec("INSERT INTO objects VALUES('outputs',?,?)", output.ID, raw); err != nil {
		return err
	}
	event := protocol.Event{ID: "output:" + output.ID, DeviceID: output.DeviceID, SessionID: output.SessionID, Kind: "file.exported", Payload: raw, CreatedAt: output.CreatedAt}
	if _, err = tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, event.DeviceID, event.SessionID, protocol.JSON(event)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Outputs(session string) ([]protocol.OutputFile, error) {
	rows, err := s.db.Query("SELECT data FROM objects WHERE bucket='outputs' AND json_extract(data,'$.sessionId')=? ORDER BY json_extract(data,'$.createdAt') DESC,id DESC LIMIT 200", session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []protocol.OutputFile{}
	for rows.Next() {
		var raw []byte
		var output protocol.OutputFile
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &output); err != nil {
			return nil, err
		}
		items = append(items, output)
	}
	return items, rows.Err()
}
