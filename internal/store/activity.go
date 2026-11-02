package store

type ActivityCounts struct {
	PendingInteractions  int
	BlockingInteractions int
	QueuedInputs         int
	UnconfirmedInputs    int
	FailedInputs         int
}

// Aggregate active rows instead of loading every historical input into Go.
func (s *Store) ActivityCounts() (map[string]ActivityCounts, error) {
	rows, err := s.db.Query(`SELECT session_id, SUM(pending), SUM(blocking), SUM(queued), SUM(unconfirmed), SUM(failed)
	FROM (
		SELECT json_extract(data,'$.sessionId') AS session_id, 1 AS pending,
			CASE WHEN json_extract(data,'$.blocking')=1 THEN 1 ELSE 0 END AS blocking,
			0 AS queued, 0 AS unconfirmed, 0 AS failed
		FROM objects WHERE bucket='interactions' AND json_extract(data,'$.state')='pending'
		UNION ALL
		SELECT json_extract(data,'$.sessionId'), 0, 0,
			CASE WHEN json_extract(data,'$.state') IN ('queued','dispatching') THEN 1 ELSE 0 END,
			CASE WHEN json_extract(data,'$.state')='unknown' THEN 1 ELSE 0 END,
			CASE WHEN json_extract(data,'$.state')='failed' THEN 1 ELSE 0 END
		FROM objects WHERE bucket='queue_items' AND json_extract(data,'$.state') IN ('queued','dispatching','unknown','failed')
	) GROUP BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]ActivityCounts)
	for rows.Next() {
		var sessionID string
		var count ActivityCounts
		if err := rows.Scan(&sessionID, &count.PendingInteractions, &count.BlockingInteractions, &count.QueuedInputs, &count.UnconfirmedInputs, &count.FailedInputs); err != nil {
			return nil, err
		}
		counts[sessionID] = count
	}
	return counts, rows.Err()
}
