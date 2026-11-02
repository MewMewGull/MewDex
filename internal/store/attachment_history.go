package store

import (
	"context"
	"errors"

	"mewdex/internal/protocol"
)

// SessionAttachments reads only positive delivery evidence. Pending commands,
// cancelled queue entries, and unreferenced uploads cannot enter model history.
func (s *Store) SessionAttachments(ctx context.Context, session string) ([]protocol.Input, error) {
	rows, err := s.db.QueryContext(ctx, `WITH delivered(request) AS (
		SELECT json_extract(data,'$.payload') FROM events WHERE session=? AND json_extract(data,'$.kind')='message.user'
		UNION ALL SELECT request FROM commands WHERE json_extract(request,'$.sessionId')=? AND json_extract(request,'$.kind') IN ('start','steer') AND json_extract(result,'$.state')='completed'
		UNION ALL SELECT data FROM objects WHERE bucket='queue_items' AND json_extract(data,'$.sessionId')=? AND coalesce(json_extract(data,'$.turnId'),'')<>''
	) SELECT DISTINCT json_extract(input.value,'$.type'),json_extract(input.value,'$.attachmentId')
	FROM delivered,json_each(delivered.request,'$.input') AS input
	WHERE json_extract(input.value,'$.type') IN ('image','file') ORDER BY 2,1 LIMIT 10001`, session, session, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	inputs := []protocol.Input{}
	for rows.Next() {
		var input protocol.Input
		if err = rows.Scan(&input.Type, &input.AttachmentID); err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(inputs) > 10000 {
		return nil, errors.New("conversation attachment history exceeds 10000 references; no partial history was used")
	}
	return inputs, nil
}
