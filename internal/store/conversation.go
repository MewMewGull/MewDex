package store

import (
	"context"

	"mewdex/internal/protocol"
)

// ConversationUpdates uses the reply-only index over canonical events. Read
// positions belong to each input application, not the executor's session state.
func (s *Store) ConversationUpdates(ctx context.Context) ([]protocol.ConversationUpdate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device,session,MAX(sequence) FROM events
		WHERE json_extract(data,'$.kind')='engine.item/completed'
		AND json_extract(data,'$.payload.item.type')='agentMessage'
		AND json_type(data,'$.payload.item.text')='text' AND json_extract(data,'$.payload.item.text')<>''
		GROUP BY device,session`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	updates := []protocol.ConversationUpdate{}
	for rows.Next() {
		var update protocol.ConversationUpdate
		if err := rows.Scan(&update.DeviceID, &update.SessionID, &update.Sequence); err != nil {
			return nil, err
		}
		updates = append(updates, update)
	}
	return updates, rows.Err()
}
