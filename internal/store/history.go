package store

import (
	"encoding/json"
	"errors"
	"math"
	"slices"

	"mewdex/internal/protocol"
)

func (s *Store) LatestSequence() (int64, error) {
	var sequence int64
	err := s.db.QueryRow("SELECT COALESCE(MAX(sequence),0) FROM events").Scan(&sequence)
	return sequence, err
}

// EventPage is a reverse page with ascending display order. The byte budget
// permits one oversized event so no event becomes impossible to retrieve.
func (s *Store) EventPage(session string, before int64, limit int) (protocol.EventPage, error) {
	return s.eventPage(session, before, limit, false)
}

// EventPageAfter reads the next chronological window without returning to live
// mode. Both directions expose the same oldest/newest cursor pair.
func (s *Store) EventPageAfter(session string, after int64, limit int) (protocol.EventPage, error) {
	return s.eventPage(session, after, limit, true)
}

func (s *Store) eventPage(session string, cursor int64, limit int, forward bool) (protocol.EventPage, error) {
	page := protocol.EventPage{Events: []protocol.Event{}}
	if session == "" || cursor < 0 || limit < 1 || limit > 1000 {
		return page, errors.New("session and a valid history page are required")
	}
	query := `SELECT sequence,data FROM events WHERE session=? AND sequence<? ORDER BY sequence DESC LIMIT ?`
	if forward {
		query = `SELECT sequence,data FROM events WHERE session=? AND sequence>? ORDER BY sequence ASC LIMIT ?`
	} else if cursor == 0 {
		cursor = math.MaxInt64
	}
	rows, err := s.db.Query(query, session, cursor, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	bytes := 0
	for rows.Next() {
		var data []byte
		var sequence int64
		if err = rows.Scan(&sequence, &data); err != nil {
			return page, err
		}
		if len(page.Events) >= limit || (len(page.Events) > 0 && bytes+len(data) > 4<<20) {
			if forward {
				page.HasNewer = true
			} else {
				page.HasMore = true
			}
			break
		}
		var event protocol.Event
		if err = json.Unmarshal(data, &event); err != nil {
			return page, err
		}
		event.Sequence = sequence
		page.Events = append(page.Events, event)
		bytes += len(data)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if err = rows.Close(); err != nil {
		return page, err
	}
	if !forward {
		slices.Reverse(page.Events)
	}
	if len(page.Events) == 0 {
		return page, nil
	}
	page.Before = page.Events[0].Sequence
	page.After = page.Events[len(page.Events)-1].Sequence
	if forward {
		err = s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE session=? AND sequence<?)`, session, page.Before).Scan(&page.HasMore)
	} else {
		err = s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE session=? AND sequence>?)`, session, page.After).Scan(&page.HasNewer)
	}
	return page, err
}
