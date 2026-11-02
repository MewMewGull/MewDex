package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"mewdex/internal/protocol"
)

// QueueItems includes all unfinished entries and a bounded recent history. The
// unfinished queue is capped on insertion; completed entries remain in SQLite.
func (s *Store) QueueItems(session string) ([]protocol.QueueItem, error) {
	rows, err := s.db.Query(`SELECT data FROM objects WHERE bucket='queue_items' AND json_extract(data,'$.sessionId')=?
	AND (json_extract(data,'$.state') IN ('queued','dispatching','running','unknown') OR id IN
	 (SELECT id FROM objects WHERE bucket='queue_items' AND json_extract(data,'$.sessionId')=? ORDER BY json_extract(data,'$.position') DESC LIMIT 50))
	ORDER BY json_extract(data,'$.position')`, session, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []protocol.QueueItem{}
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		var item protocol.QueueItem
		if err = json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// PutQueueItem commits progress and its outbox event together before an engine
// side effect. A crash cannot turn a dispatched entry back into a queued entry.
func (s *Store) PutQueueItem(item protocol.QueueItem) (protocol.QueueItem, protocol.Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return item, protocol.Event{}, err
	}
	defer tx.Rollback()
	if item.Position == 0 {
		var pending int
		err = tx.QueryRow(`SELECT COUNT(*) FROM objects WHERE bucket='queue_items' AND json_extract(data,'$.sessionId')=? AND json_extract(data,'$.state') IN ('queued','dispatching','running','unknown')`, item.SessionID).Scan(&pending)
		if err != nil {
			return item, protocol.Event{}, err
		}
		if pending >= 100 {
			return item, protocol.Event{}, errors.New("queue has 100 unfinished entries")
		}
		err = tx.QueryRow(`SELECT COALESCE(MAX(json_extract(data,'$.position')),0)+1 FROM objects WHERE bucket='queue_items' AND json_extract(data,'$.sessionId')=?`, item.SessionID).Scan(&item.Position)
		if err != nil {
			return item, protocol.Event{}, err
		}
	}
	if err = projectQueueItem(tx, item); err != nil {
		return item, protocol.Event{}, err
	}
	event := protocol.Event{ID: protocol.ID(), DeviceID: item.DeviceID, SessionID: item.SessionID, Kind: "queue.item.updated", Payload: protocol.JSON(item), CreatedAt: protocol.Now()}
	res, err := tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, event.DeviceID, event.SessionID, protocol.JSON(event))
	if err != nil {
		return item, event, err
	}
	event.Sequence, err = res.LastInsertId()
	if err != nil {
		return item, event, err
	}
	return item, event, tx.Commit()
}

func projectQueueItem(tx *sql.Tx, item protocol.QueueItem) error {
	if item.ID == "" || item.SessionID == "" || item.Position < 1 || item.Revision < 1 || !slices.Contains([]string{"queued", "dispatching", "running", "completed", "interrupted", "failed", "unknown", "cancelled"}, item.State) {
		return errors.New("invalid queue item")
	}
	var data []byte
	if err := tx.QueryRow("SELECT data FROM objects WHERE bucket='sessions' AND id=?", item.SessionID).Scan(&data); err != nil {
		return err
	}
	var session protocol.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}
	if session.DeviceID != item.DeviceID || session.ProjectID != item.ProjectID {
		return errors.New("queue item belongs to another execution target")
	}
	err := tx.QueryRow("SELECT data FROM objects WHERE bucket='queue_items' AND id=?", item.ID).Scan(&data)
	if err == nil {
		var old protocol.QueueItem
		if err = json.Unmarshal(data, &old); err != nil {
			return err
		}
		immutable := item
		immutable.State, immutable.Revision, immutable.TurnID, immutable.Error, immutable.UpdatedAt = old.State, old.Revision, old.TurnID, old.Error, old.UpdatedAt
		if string(protocol.JSON(immutable)) != string(protocol.JSON(old)) {
			return errors.New("queued input and policy are immutable")
		}
		if item.Revision < old.Revision {
			return nil // A startup snapshot may follow an older unacked event.
		}
		if item.Revision == old.Revision {
			if string(protocol.JSON(item)) != string(data) {
				return ErrConflict
			}
			return nil
		}
		if !queueTransition(old.State, item.State) {
			return errors.New("invalid queue state transition")
		}
		if old.TurnID != "" && old.TurnID != item.TurnID {
			return errors.New("queue turn is immutable once assigned")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec("INSERT INTO objects VALUES('queue_items',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", item.ID, protocol.JSON(item))
	return err
}

func queueTransition(old, next string) bool {
	switch old {
	case "queued":
		return next == "dispatching" || next == "cancelled"
	case "dispatching":
		return slices.Contains([]string{"running", "unknown", "failed"}, next)
	case "running":
		return slices.Contains([]string{"completed", "interrupted", "failed", "unknown"}, next)
	case "unknown":
		return slices.Contains([]string{"running", "completed", "interrupted", "failed"}, next)
	case "failed":
		return next == "cancelled"
	}
	return false
}
