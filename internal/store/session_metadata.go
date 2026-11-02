package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"mewdex/internal/protocol"
)

var ErrInvalidSessionTitle = errors.New("对话名称需为 1 到 120 个字符")

type sessionMetadata struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title,omitempty"`
	Archived  bool   `json:"archived"`
	Pinned    bool   `json:"pinned"`
	UpdatedAt string `json:"updatedAt"`
}

func applySessionMetadata(session protocol.Session, metadata sessionMetadata) protocol.Session {
	if metadata.Title != "" {
		session.Title = metadata.Title
	}
	session.Archived = metadata.Archived
	session.Pinned = metadata.Pinned
	return session
}

func (s *Store) SessionView(session protocol.Session) (protocol.Session, error) {
	var metadata sessionMetadata
	if err := s.Get("session_metadata", session.ID, &metadata); err != nil && !errors.Is(err, ErrNotFound) {
		return session, err
	}
	return applySessionMetadata(session, metadata), nil
}

func (s *Store) SessionViews(sessions []protocol.Session) ([]protocol.Session, error) {
	metadata, err := List[sessionMetadata](s, "session_metadata")
	if err != nil {
		return nil, err
	}
	byID := make(map[string]sessionMetadata, len(metadata))
	for _, item := range metadata {
		byID[item.SessionID] = item
	}
	for i, session := range sessions {
		sessions[i] = applySessionMetadata(session, byID[session.ID])
	}
	return sessions, nil
}

func (s *Store) UpdateSession(id string, patch protocol.SessionPatch) (protocol.Session, bool, error) {
	var session protocol.Session
	tx, err := s.db.Begin()
	if err != nil {
		return session, false, err
	}
	defer tx.Rollback()
	var data []byte
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='sessions' AND id=?", id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return session, false, err
	}
	if err = json.Unmarshal(data, &session); err != nil {
		return session, false, err
	}
	metadata := sessionMetadata{SessionID: id}
	err = tx.QueryRow("SELECT data FROM objects WHERE bucket='session_metadata' AND id=?", id).Scan(&data)
	if err == nil {
		err = json.Unmarshal(data, &metadata)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return session, false, err
	}
	previous := metadata
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" || utf8.RuneCountInString(title) > 120 {
			return session, false, ErrInvalidSessionTitle
		}
		metadata.Title = title
	}
	if patch.Archived != nil {
		metadata.Archived = *patch.Archived
	}
	if patch.Pinned != nil {
		metadata.Pinned = *patch.Pinned
	}
	view := applySessionMetadata(session, metadata)
	if previous == metadata {
		return view, false, nil
	}
	metadata.UpdatedAt = protocol.Now()
	if _, err = tx.Exec("INSERT INTO objects VALUES('session_metadata',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", id, protocol.JSON(metadata)); err != nil {
		return session, false, err
	}
	event := protocol.Event{ID: protocol.ID(), DeviceID: session.DeviceID, SessionID: id, Kind: "session.metadata.updated", Payload: protocol.JSON(metadata), CreatedAt: metadata.UpdatedAt}
	if _, err = tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, event.DeviceID, id, protocol.JSON(event)); err != nil {
		return session, false, err
	}
	if err = tx.Commit(); err != nil {
		return session, false, err
	}
	return view, true, nil
}
