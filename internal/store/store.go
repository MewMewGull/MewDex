package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mewdex/internal/protocol"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("identifier already used with different content")

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;
	CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL);
	INSERT INTO schema_version SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM schema_version);
	CREATE TABLE IF NOT EXISTS objects(bucket TEXT NOT NULL,id TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(bucket,id));
	CREATE TABLE IF NOT EXISTS events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,device TEXT NOT NULL,session TEXT NOT NULL,data BLOB NOT NULL,acked INTEGER NOT NULL DEFAULT 0);
	CREATE TABLE IF NOT EXISTS commands(id TEXT PRIMARY KEY,request BLOB NOT NULL,result BLOB NOT NULL);
	CREATE TABLE IF NOT EXISTS identities(hash TEXT PRIMARY KEY,data BLOB NOT NULL);
	CREATE TABLE IF NOT EXISTS invitations(hash TEXT PRIMARY KEY,role TEXT NOT NULL,expires INTEGER NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	var version int
	if err = db.QueryRow("SELECT version FROM schema_version").Scan(&version); err != nil || (version != 1 && version != 2) {
		db.Close()
		return nil, fmt.Errorf("unsupported database version %d: %v", version, err)
	}
	if version == 1 {
		tx, err := db.Begin()
		if err != nil {
			db.Close()
			return nil, err
		}
		_, err = tx.Exec(`ALTER TABLE invitations ADD COLUMN access BLOB;
		UPDATE identities SET data=json_set(data,'$.access',json_object('allDevices',json(CASE WHEN json_extract(data,'$.role') IN ('admin','client') THEN 'true' ELSE 'false' END),'targets',json('[]'),'gateway',json('false'))) WHERE json_type(data,'$.access') IS NULL;
		UPDATE schema_version SET version=2;`)
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err = db.Exec(`CREATE INDEX IF NOT EXISTS queue_session_position ON objects(json_extract(data,'$.sessionId'),json_extract(data,'$.position')) WHERE bucket='queue_items';
	CREATE INDEX IF NOT EXISTS interaction_activity ON objects(json_extract(data,'$.state'),json_extract(data,'$.sessionId')) WHERE bucket='interactions';
	CREATE INDEX IF NOT EXISTS queue_activity ON objects(json_extract(data,'$.state'),json_extract(data,'$.sessionId')) WHERE bucket='queue_items';
	CREATE INDEX IF NOT EXISTS events_session_sequence ON events(session,sequence);
	CREATE INDEX IF NOT EXISTS events_task_plan ON events(session,device,sequence DESC) WHERE json_extract(data,'$.kind')='engine.turn/plan/updated';
	CREATE INDEX IF NOT EXISTS events_turn_lifecycle ON events(session,device,sequence DESC) WHERE json_extract(data,'$.kind') IN ('engine.turn/started','engine.turn/completed');
	CREATE INDEX IF NOT EXISTS events_conversation_reply ON events(device,session,sequence DESC)
	WHERE json_extract(data,'$.kind')='engine.item/completed'
	AND json_extract(data,'$.payload.item.type')='agentMessage'
	AND json_type(data,'$.payload.item.text')='text' AND json_extract(data,'$.payload.item.text')<>'';
	CREATE INDEX IF NOT EXISTS commands_completed_session ON commands(json_extract(request,'$.sessionId')) WHERE json_extract(result,'$.state')='completed';
	CREATE INDEX IF NOT EXISTS commands_pending_source_device ON commands(json_extract(request,'$.sourceId'),json_extract(request,'$.deviceId')) WHERE json_extract(result,'$.state') IN ('accepted','unknown');`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Put(bucket, id string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO objects VALUES(?,?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", bucket, id, b)
	return err
}
func (s *Store) Get(bucket, id string, value any) error {
	var b []byte
	err := s.db.QueryRow("SELECT data FROM objects WHERE bucket=? AND id=?", bucket, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}
func (s *Store) Delete(bucket, id string) error {
	_, err := s.db.Exec("DELETE FROM objects WHERE bucket=? AND id=?", bucket, id)
	return err
}
func (s *Store) List(bucket string) ([]json.RawMessage, error) {
	rows, err := s.db.Query("SELECT data FROM objects WHERE bucket=? ORDER BY id", bucket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
func List[T any](s *Store, bucket string) ([]T, error) {
	raw, err := s.List(bucket)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(raw))
	for _, b := range raw {
		var v T
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Store) Append(e protocol.Event) (protocol.Event, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	_, err = s.db.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING", e.ID, e.DeviceID, e.SessionID, b)
	if err != nil {
		return e, err
	}
	var stored []byte
	err = s.db.QueryRow("SELECT sequence,data FROM events WHERE id=?", e.ID).Scan(&e.Sequence, &stored)
	if err != nil {
		return e, err
	}
	seq := e.Sequence
	if err = json.Unmarshal(stored, &e); err != nil {
		return e, err
	}
	e.Sequence = seq
	return e, nil
}
func (s *Store) Events(after int64, session string, limit int) ([]protocol.Event, error) {
	if limit < 1 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.Query("SELECT sequence,data FROM events WHERE sequence>? AND (?='' OR session=?) ORDER BY sequence LIMIT ?", after, session, session, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.Event{}
	for rows.Next() {
		var seq int64
		var b []byte
		if err = rows.Scan(&seq, &b); err != nil {
			return nil, err
		}
		var e protocol.Event
		if err = json.Unmarshal(b, &e); err != nil {
			return nil, err
		}
		e.Sequence = seq
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) Unacked() ([]protocol.Event, error) {
	rows, err := s.db.Query("SELECT data FROM events WHERE acked=0 ORDER BY sequence LIMIT 200")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.Event{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var e protocol.Event
		if err = json.Unmarshal(b, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) Ack(id string) error {
	_, err := s.db.Exec("UPDATE events SET acked=1 WHERE id=?", id)
	return err
}

func (s *Store) HasStarted(session string) (bool, error) {
	var yes bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM events WHERE session=? AND json_extract(data,'$.kind') IN ('message.user','engine.turn/started'))", session).Scan(&yes)
	return yes, err
}

// Claim persists ownership before an external side effect. Unknown outcomes are never replayed.
func (s *Store) Claim(c protocol.Command) (protocol.CommandResult, bool, error) {
	b, err := ledgerRequest(c, false)
	if err != nil {
		return protocol.CommandResult{}, false, err
	}
	initial := protocol.CommandResult{CommandID: c.ID, State: "accepted"}
	r, _ := json.Marshal(initial)
	res, err := s.db.Exec("INSERT INTO commands(id,request,result) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING", c.ID, b, r)
	if err != nil {
		return initial, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return initial, false, err
	}
	if n == 1 {
		return initial, true, nil
	}
	var prior, data []byte
	if err = s.db.QueryRow("SELECT request,result FROM commands WHERE id=?", c.ID).Scan(&prior, &data); err != nil {
		return initial, false, err
	}
	// Retry timestamps differ; a different authenticated source must not reuse this ID.
	var old protocol.Command
	if err = json.Unmarshal(prior, &old); err != nil {
		return initial, false, err
	}
	old.CreatedAt = c.CreatedAt
	priorRequest, err := ledgerRequest(old, true)
	if err != nil {
		return initial, false, err
	}
	if string(priorRequest) != string(b) {
		return initial, false, ErrConflict
	}
	err = json.Unmarshal(data, &initial)
	return initial, false, err
}

// User-input replies can contain secret form fields. The receipt ledger needs
// their fingerprint for idempotency, never the answer text itself.
func ledgerRequest(c protocol.Command, stored bool) ([]byte, error) {
	if c.Kind == "respond" && len(c.Response) > 0 {
		var marker map[string]string
		redacted := stored && json.Unmarshal(c.Response, &marker) == nil && len(marker) == 1 && len(marker["mewdexResponseSHA256"]) == 64
		if !redacted {
			body, err := json.Marshal(c.Response)
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(body)
			c.Response = protocol.JSON(map[string]string{"mewdexResponseSHA256": hex.EncodeToString(sum[:])})
		}
	}
	return json.Marshal(c)
}
func (s *Store) Finish(r protocol.CommandResult) error {
	_, err := s.db.Exec("UPDATE commands SET result=? WHERE id=?", protocol.JSON(r), r.CommandID)
	return err
}
func (s *Store) Result(id string) (protocol.CommandResult, error) {
	var r protocol.CommandResult
	var b []byte
	err := s.db.QueryRow("SELECT result FROM commands WHERE id=?", id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(b, &r)
	return r, err
}
func Hash(token string) string { b := sha256.Sum256([]byte(token)); return hex.EncodeToString(b[:]) }
func (s *Store) Invite(role string, ttl time.Duration) (string, error) {
	return s.InviteWithAccess(role, protocol.DefaultAccess(role), ttl)
}

func (s *Store) InviteWithAccess(role string, access protocol.Access, ttl time.Duration) (string, error) {
	if role != "client" && role != "executor" && role != "admin" {
		return "", errors.New("invalid identity role")
	}
	code := protocol.ID()
	if role == "admin" {
		access = protocol.DefaultAccess("admin")
	}
	access = access.Normalized()
	_, err := s.db.Exec("INSERT INTO invitations(hash,role,expires,access) VALUES(?,?,?,?)", Hash(code), role, time.Now().Add(ttl).Unix(), protocol.JSON(access))
	return code, err
}
func (s *Store) Pair(code, name, device string, reservedDevices ...string) (string, protocol.Identity, error) {
	var id protocol.Identity
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return "", id, err
	}
	defer tx.Rollback()
	var role string
	var accessJSON []byte
	err = tx.QueryRow("DELETE FROM invitations WHERE hash=? AND expires>? RETURNING role,access", Hash(code), time.Now().Unix()).Scan(&role, &accessJSON)
	if err != nil {
		return "", id, errors.New("invalid or expired pairing code")
	}
	if role == "executor" && device == "" {
		return "", id, errors.New("executor pairing requires deviceId")
	}
	access := protocol.DefaultAccess(role)
	if len(accessJSON) > 0 {
		if err = json.Unmarshal(accessJSON, &access); err != nil {
			return "", id, err
		}
	}
	if role == "executor" {
		for _, reserved := range reservedDevices {
			if device == reserved {
				return "", id, errors.New("executor deviceId is reserved by the Hub")
			}
		}
		var count int
		if err = tx.QueryRow("SELECT count(*) FROM identities WHERE json_extract(data,'$.role')='executor' AND json_extract(data,'$.deviceId')=?", device).Scan(&count); err != nil {
			return "", id, err
		}
		if count > 0 {
			return "", id, errors.New("executor deviceId is already paired; revoke its previous identity before pairing again")
		}
	}
	if device == "" {
		device = protocol.ID()
	}
	id = protocol.Identity{ID: protocol.ID(), Name: name, Role: role, DeviceID: device, CreatedAt: protocol.Now(), Access: access.Normalized()}
	token := protocol.ID() + protocol.ID()
	if _, err = tx.Exec("INSERT INTO identities VALUES(?,?)", Hash(token), protocol.JSON(id)); err != nil {
		return "", id, err
	}
	err = tx.Commit()
	return token, id, err
}
func (s *Store) Authenticate(token string) (protocol.Identity, error) {
	var id protocol.Identity
	var b []byte
	err := s.db.QueryRow("SELECT data FROM identities WHERE hash=?", Hash(token)).Scan(&b)
	if err != nil {
		return id, errors.New("unauthorized")
	}
	err = json.Unmarshal(b, &id)
	return id, err
}
func (s *Store) Revoke(id string) error {
	_, err := s.db.Exec("DELETE FROM identities WHERE json_extract(data,'$.id')=?", id)
	return err
}

func (s *Store) Identities() ([]protocol.Identity, error) {
	rows, err := s.db.Query("SELECT data FROM identities ORDER BY json_extract(data,'$.createdAt')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []protocol.Identity{}
	seen := map[string]bool{}
	for rows.Next() {
		var b []byte
		var id protocol.Identity
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &id); err != nil {
			return nil, err
		}
		if !seen[id.ID] {
			out = append(out, id)
			seen[id.ID] = true
		}
	}
	return out, rows.Err()
}

func (s *Store) UpdateAccess(id string, access protocol.Access) error {
	identity, err := s.Identity(id)
	if err != nil {
		return err
	}
	if identity.Role == "admin" {
		return errors.New("admin access is managed by its role")
	}
	identity.Access = access.Normalized()
	_, err = s.db.Exec("UPDATE identities SET data=? WHERE json_extract(data,'$.id')=?", protocol.JSON(identity), id)
	return err
}

func (s *Store) Command(id string) (protocol.Command, error) {
	var c protocol.Command
	var b []byte
	err := s.db.QueryRow("SELECT request FROM commands WHERE id=?", id).Scan(&b)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}

func (s *Store) Identity(id string) (protocol.Identity, error) {
	var identity protocol.Identity
	var b []byte
	err := s.db.QueryRow("SELECT data FROM identities WHERE json_extract(data,'$.id')=?", id).Scan(&b)
	if err != nil {
		return identity, errors.New("identity revoked or missing")
	}
	err = json.Unmarshal(b, &identity)
	return identity, err
}

// Ingest atomically stores a remote event and updates its query projections once.
func (s *Store) Ingest(e protocol.Event) (protocol.Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	res, err := tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING", e.ID, e.DeviceID, e.SessionID, protocol.JSON(e))
	if err != nil {
		return e, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return e, err
	}
	if count == 1 {
		if e.SessionID != "" && e.Kind != "session.updated" {
			var sessionData []byte
			if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='sessions' AND id=?", e.SessionID).Scan(&sessionData); err != nil {
				return e, err
			}
			var session protocol.Session
			if err = json.Unmarshal(sessionData, &session); err != nil {
				return e, err
			}
			if session.DeviceID != e.DeviceID {
				return e, errors.New("event session belongs to another device")
			}
		}
		switch e.Kind {
		case "workspace.catalog.updated":
			if e.SessionID != "" {
				return e, errors.New("workspace catalogue cannot belong to a conversation")
			}
			var projects []protocol.Project
			if err = json.Unmarshal(e.Payload, &projects); err != nil {
				return e, err
			}
			var data []byte
			if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='devices' AND id=?", e.DeviceID).Scan(&data); err != nil {
				return e, err
			}
			var device protocol.Device
			if err = json.Unmarshal(data, &device); err != nil {
				return e, err
			}
			device.Projects = projects
			device, err = applyProjectMetadata(tx, device)
			if err != nil {
				return e, err
			}
			_, err = tx.Exec("UPDATE objects SET data=? WHERE bucket='devices' AND id=?", protocol.JSON(device), e.DeviceID)
		case "queue.item.updated":
			var item protocol.QueueItem
			if err = json.Unmarshal(e.Payload, &item); err != nil {
				return e, err
			}
			if item.DeviceID != e.DeviceID || item.SessionID != e.SessionID {
				return e, errors.New("invalid queue event ownership")
			}
			err = projectQueueItem(tx, item)
		case "session.updated":
			var ss protocol.Session
			if err = json.Unmarshal(e.Payload, &ss); err != nil {
				return e, err
			}
			if ss.ID != e.SessionID || ss.DeviceID != e.DeviceID {
				return e, errors.New("invalid session event ownership")
			}
			var prior []byte
			queryErr := tx.QueryRow("SELECT data FROM objects WHERE bucket='sessions' AND id=?", ss.ID).Scan(&prior)
			if queryErr == nil {
				var old protocol.Session
				if err = json.Unmarshal(prior, &old); err != nil {
					return e, err
				}
				if old.DeviceID != ss.DeviceID || old.ProjectID != ss.ProjectID {
					return e, errors.New("session execution device and project are immutable")
				}
			} else if !errors.Is(queryErr, sql.ErrNoRows) {
				return e, queryErr
			}
			_, err = tx.Exec("INSERT INTO objects VALUES('sessions',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", ss.ID, protocol.JSON(ss))
		case "interaction.updated":
			var request protocol.Interaction
			if err = json.Unmarshal(e.Payload, &request); err != nil {
				return e, err
			}
			if request.ID == "" || request.SessionID != e.SessionID {
				return e, errors.New("invalid interaction ownership")
			}
			var sessionData []byte
			if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='sessions' AND id=?", e.SessionID).Scan(&sessionData); err != nil {
				return e, err
			}
			var session protocol.Session
			if err = json.Unmarshal(sessionData, &session); err != nil {
				return e, err
			}
			if session.DeviceID != e.DeviceID {
				return e, errors.New("interaction belongs to another device")
			}
			var prior []byte
			queryErr := tx.QueryRow("SELECT data FROM objects WHERE bucket='interactions' AND id=?", request.ID).Scan(&prior)
			if queryErr == nil {
				var old protocol.Interaction
				if err = json.Unmarshal(prior, &old); err != nil {
					return e, err
				}
				if err = interactionTransition(old, request); err != nil {
					return e, err
				}
			} else if !errors.Is(queryErr, sql.ErrNoRows) {
				return e, queryErr
			}
			_, err = tx.Exec("INSERT INTO objects VALUES('interactions',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", request.ID, protocol.JSON(request))
		case "policy.snapshot":
			var snapshot protocol.PolicySnapshot
			if err = json.Unmarshal(e.Payload, &snapshot); err != nil {
				return e, err
			}
			// Earlier development events omitted these envelope-derived fields.
			if snapshot.SessionID == "" {
				snapshot.SessionID = e.SessionID
			}
			if snapshot.ID == "" {
				snapshot.ID = snapshot.SessionID + ":" + snapshot.Version
			}
			if snapshot.CreatedAt == "" {
				snapshot.CreatedAt = e.CreatedAt
			}
			if snapshot.SessionID != e.SessionID || snapshot.ID != snapshot.SessionID+":"+snapshot.Version || snapshot.Version == "" {
				return e, errors.New("invalid policy snapshot ownership")
			}
			var prior []byte
			queryErr := tx.QueryRow("SELECT data FROM objects WHERE bucket='policies' AND id=?", snapshot.ID).Scan(&prior)
			if queryErr == nil {
				if string(prior) != string(protocol.JSON(snapshot)) {
					return e, errors.New("policy snapshot is immutable")
				}
			} else if !errors.Is(queryErr, sql.ErrNoRows) {
				return e, queryErr
			}
			_, err = tx.Exec("INSERT INTO objects VALUES('policies',?,?) ON CONFLICT(bucket,id) DO NOTHING", snapshot.ID, protocol.JSON(snapshot))
		case "command.result":
			var result protocol.CommandResult
			if err = json.Unmarshal(e.Payload, &result); err != nil {
				return e, err
			}
			var request []byte
			if err = tx.QueryRow("SELECT request FROM commands WHERE id=?", result.CommandID).Scan(&request); err != nil {
				return e, err
			}
			var cmd protocol.Command
			if err = json.Unmarshal(request, &cmd); err != nil {
				return e, err
			}
			if cmd.DeviceID != e.DeviceID {
				return e, errors.New("command result belongs to another device")
			}
			_, err = tx.Exec("UPDATE commands SET result=? WHERE id=?", protocol.JSON(result), result.CommandID)
		}
		if err != nil {
			return e, err
		}
	}
	var data []byte
	originDevice, originSession := e.DeviceID, e.SessionID
	if err = tx.QueryRow("SELECT sequence,data FROM events WHERE id=?", e.ID).Scan(&e.Sequence, &data); err != nil {
		return e, err
	}
	seq := e.Sequence
	if err = json.Unmarshal(data, &e); err != nil {
		return e, err
	}
	if e.DeviceID != originDevice || e.SessionID != originSession {
		return e, errors.New("event ID belongs to another source")
	}
	e.Sequence = seq
	err = tx.Commit()
	return e, err
}

func interactionTransition(old, next protocol.Interaction) error {
	state := next.State
	next.State = old.State
	if string(protocol.JSON(next)) != string(protocol.JSON(old)) {
		return errors.New("interaction request is immutable")
	}
	if state == old.State {
		return nil
	}
	if old.State == "pending" && (state == "responding" || state == "expired") {
		return nil
	}
	if old.State == "responding" && (state == "answered" || state == "unknown" || state == "expired") {
		return nil
	}
	return errors.New("invalid interaction state transition")
}
