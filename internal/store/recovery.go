package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"mewdex/internal/protocol"
)

// RecoverCommands runs only during service startup, before accepting commands.
// Durable creation/enqueue/message evidence can recover a receipt; every other
// previously accepted command becomes unknown and is never dispatched again.
func (s *Store) RecoverCommands(device string) ([]protocol.CommandResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT request FROM commands WHERE json_extract(result,'$.state')='accepted' AND (?='' OR json_extract(request,'$.deviceId')=?)`, device, device)
	if err != nil {
		return nil, err
	}
	commands := []protocol.Command{}
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return nil, err
		}
		var command protocol.Command
		if err = json.Unmarshal(data, &command); err != nil {
			rows.Close()
			return nil, err
		}
		commands = append(commands, command)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	results := make([]protocol.CommandResult, 0, len(commands))
	for _, c := range commands {
		result := protocol.CommandResult{CommandID: c.ID, State: "unknown", Error: "service restarted before its receipt was persisted; command will not be replayed"}
		sessionID := c.SessionID
		if c.Kind == "create" {
			sessionID = "s_" + c.ID
		}
		var sessionData []byte
		lookup := tx.QueryRow("SELECT data FROM objects WHERE bucket='sessions' AND id=?", sessionID).Scan(&sessionData)
		if lookup == nil {
			var session protocol.Session
			if err = json.Unmarshal(sessionData, &session); err != nil {
				return nil, err
			}
			if session.DeviceID == c.DeviceID {
				if c.Kind == "execution.retire" {
					confirmed, err := recoveredRetirement(tx, c, session)
					if err != nil {
						return nil, err
					}
					if confirmed {
						result.State, result.Error, result.Session = "completed", "", &session
					}
				}
				if c.Kind == "create" && session.ProjectID == c.ProjectID {
					result.State, result.Error, result.Session = "completed", "", &session
				}
				if c.Kind == "start" && session.LastStartID == c.ID {
					var delivered bool
					if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE session=? AND json_extract(data,'$.kind')='message.user' AND json_extract(data,'$.payload.id')=?)`, session.ID, c.ID).Scan(&delivered); err != nil {
						return nil, err
					}
					if delivered {
						result.State, result.Error, result.Session = "completed", "", &session
					}
				}
				if c.Kind == "enqueue" {
					var itemData []byte
					itemErr := tx.QueryRow("SELECT data FROM objects WHERE bucket='queue_items' AND id=?", c.ID).Scan(&itemData)
					if itemErr == nil {
						var item protocol.QueueItem
						if err = json.Unmarshal(itemData, &item); err != nil {
							return nil, err
						}
						if item.SessionID == session.ID && item.DeviceID == c.DeviceID {
							result.State, result.Error, result.Session, result.Data = "completed", "", &session, protocol.JSON(item)
						}
					} else if !errors.Is(itemErr, sql.ErrNoRows) {
						return nil, itemErr
					}
				}
			}
		} else if !errors.Is(lookup, sql.ErrNoRows) {
			return nil, lookup
		}
		if _, err = tx.Exec("UPDATE commands SET result=? WHERE id=?", protocol.JSON(result), c.ID); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, tx.Commit()
}
