package store

import "encoding/json"

// Object is one entry in an atomic checkpoint (for example, a delivery outbox
// and the event cursor that produced it).
type Object struct {
	Bucket string
	ID     string
	Value  any
}

func (s *Store) PutObjects(objects ...Object) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, object := range objects {
		data, err := json.Marshal(object.Value)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", object.Bucket, object.ID, data); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// InsertObject claims an inbound platform ID before acknowledging its delivery.
func (s *Store) InsertObject(bucket, id string, value any) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	result, err := s.db.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(bucket,id) DO NOTHING", bucket, id, data)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

// PendingObjects selects the oldest waiting entry per conversation. One busy
// chat cannot occupy the entire scan and starve other conversations.
func (s *Store) PendingObjects(bucket string, limit int) ([]json.RawMessage, error) {
	rows, err := s.db.Query(`SELECT data FROM (
		SELECT id,data,ROW_NUMBER() OVER (PARTITION BY json_extract(data,'$.bindingId') ORDER BY json_extract(data,'$.createdAt'),id) AS position
		FROM objects WHERE bucket=? AND json_extract(data,'$.state')='pending'
	) WHERE position=1 ORDER BY json_extract(data,'$.createdAt'),id LIMIT ?`, bucket, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		result = append(result, json.RawMessage(data))
	}
	return result, rows.Err()
}
