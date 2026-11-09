package store

import "mewdex/internal/protocol"

// PendingCommands reads metadata directly, without loading potentially large
// input bodies or secret interaction-response fingerprints into the response.
// Account status reads have no action to reconcile; their receipts stay queryable.
func (s *Store) PendingCommands(source, device string, after int64, limit int) (protocol.PendingCommandPage, error) {
	if limit < 1 || limit > 200 {
		limit = 20
	}
	page := protocol.PendingCommandPage{Commands: []protocol.CommandReference{}, After: after}
	rows, err := s.db.Query(`SELECT rowid,id,json_extract(request,'$.deviceId'),
	COALESCE(json_extract(request,'$.sessionId'),''),COALESCE(json_extract(request,'$.projectId'),''),
	json_extract(request,'$.kind'),json_extract(result,'$.state'),COALESCE(json_extract(request,'$.createdAt'),'')
	FROM commands WHERE json_extract(request,'$.sourceId')=? AND json_extract(request,'$.deviceId')=?
	AND json_extract(request,'$.kind') NOT IN ('account.read','file.list','model.list','skill.list','extension.read')
	AND json_extract(result,'$.state') IN ('accepted','unknown') AND rowid>? ORDER BY rowid LIMIT ?`, source, device, after, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var cursor int64
		var reference protocol.CommandReference
		if err = rows.Scan(&cursor, &reference.ID, &reference.DeviceID, &reference.SessionID, &reference.ProjectID, &reference.Kind, &reference.State, &reference.CreatedAt); err != nil {
			return page, err
		}
		if len(page.Commands) == limit {
			page.HasMore = true
			break
		}
		page.Commands = append(page.Commands, reference)
		page.After = cursor
	}
	return page, rows.Err()
}
