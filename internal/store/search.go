package store

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"mewdex/internal/protocol"
)

var ErrInvalidSearch = errors.New("a conversation, search text up to 200 characters and valid search page are required")

// SearchMessages reads durable user input and complete assistant text. It does
// not search raw event JSON, tool output, account data or interaction answers.
func (s *Store) SearchMessages(ctx context.Context, session protocol.Session, query string, before int64, limit int) (protocol.MessageSearchPage, error) {
	page := protocol.MessageSearchPage{Matches: []protocol.MessageMatch{}}
	query = strings.TrimSpace(query)
	if session.ID == "" || session.DeviceID == "" || query == "" || utf8.RuneCountInString(query) > 200 || before < 0 || limit < 1 || limit > 100 {
		return page, ErrInvalidSearch
	}
	if before == 0 {
		before = math.MaxInt64
	}
	// Choose the latest complete item before applying the page cursor, matching
	// the conversation renderer when an item is reported more than once.
	rows, err := s.db.QueryContext(ctx, `WITH candidates AS (
		SELECT sequence,CASE json_extract(data,'$.kind')
			WHEN 'message.user' THEN 'user:' || id
			ELSE 'assistant:' || COALESCE(json_extract(data,'$.payload.item.id'),id) END AS message_key
		FROM events WHERE session=? AND device=? AND (
			json_extract(data,'$.kind')='message.user' OR (
				json_extract(data,'$.kind')='engine.item/completed' AND
				json_extract(data,'$.payload.item.type')='agentMessage'))
	), latest AS (SELECT MAX(sequence) AS sequence FROM candidates GROUP BY message_key)
	SELECT e.sequence,e.data FROM latest JOIN events e ON e.sequence=latest.sequence
	WHERE e.sequence<? ORDER BY e.sequence DESC`, session.ID, session.DeviceID, before)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	needle := strings.ToLower(query)
	for rows.Next() {
		var raw []byte
		var sequence int64
		if err = rows.Scan(&sequence, &raw); err != nil {
			return page, err
		}
		var event protocol.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return page, err
		}
		var payload struct {
			Input []protocol.Input `json:"input"`
			Item  struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err = json.Unmarshal(event.Payload, &payload); err != nil {
			return page, err
		}
		id, role, text := event.ID, "user", ""
		if event.Kind == "message.user" {
			parts := []string{}
			for _, input := range payload.Input {
				if input.Type == "text" {
					parts = append(parts, input.Text)
				}
			}
			text = strings.Join(parts, "\n")
		} else {
			id, role, text = payload.Item.ID, "assistant", payload.Item.Text
			if id == "" {
				id = event.ID
			}
		}
		excerpt, found := messageExcerpt(text, needle)
		if !found {
			continue
		}
		if len(page.Matches) == limit {
			page.HasMore = true
			break
		}
		page.Matches = append(page.Matches, protocol.MessageMatch{ID: id, Sequence: sequence, Role: role, Excerpt: excerpt, CreatedAt: event.CreatedAt})
		page.Before = sequence
	}
	return page, rows.Err()
}

func messageExcerpt(text, needle string) (protocol.SearchExcerpt, bool) {
	lower := strings.ToLower(text)
	position := strings.Index(lower, needle)
	if position < 0 {
		return protocol.SearchExcerpt{}, false
	}
	start := utf8.RuneCountInString(lower[:position])
	end := start + utf8.RuneCountInString(needle)
	runes := []rune(text)
	left, right := max(0, start-48), min(len(runes), end+96)
	excerpt := protocol.SearchExcerpt{Before: string(runes[left:start]), Match: string(runes[start:end]), After: string(runes[end:right])}
	if left > 0 {
		excerpt.Before = "…" + excerpt.Before
	}
	if right < len(runes) {
		excerpt.After += "…"
	}
	return excerpt, true
}
