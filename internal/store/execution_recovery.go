package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"mewdex/internal/protocol"
)

type RestartEvidence struct {
	ID          string `json:"id"`
	DeviceID    string `json:"deviceId"`
	BootSession string `json:"bootSession"`
	RestartFrom string `json:"restartFrom"`
	PlanSHA256  string `json:"planSha256"`
}
type executionCandidate struct {
	ID         string `json:"id"`
	EvidenceID string `json:"evidenceId"`
	DeviceID   string `json:"deviceId"`
	SessionID  string `json:"sessionId"`
	Attempt    string `json:"attempt"`
}
type executionOrigin struct {
	BootSession string `json:"bootSession"`
	Attempt     string `json:"attempt"`
}
type retirementReceipt struct {
	DeviceID  string `json:"deviceId"`
	SessionID string `json:"sessionId"`
	SourceID  string `json:"sourceId"`
	ReviewID  string `json:"reviewId"`
	Attempt   string `json:"attempt"`
}

func executionAttempt(s protocol.Session) string {
	// Startup may normalize state, freeze queues and rebind a permission scope.
	// Reconciliation can discover or clear the old turn ID without submitting
	// input. Every new turn first persists a new LastStartID, so that identifier
	// binds this proof to the attempt while allowing read-only reconciliation.
	return Hash(string(protocol.JSON(struct{ Device, Session, Project, Thread, Start string }{s.DeviceID, s.ID, s.ProjectID, s.EngineID, s.LastStartID})))
}

func (s *Store) RecordExecutionOrigin(session protocol.Session, boot string) error {
	if session.LastStartID == "" || boot == "" {
		return nil
	}
	return s.Put("execution_origins", session.LastStartID, executionOrigin{BootSession: boot, Attempt: executionAttempt(session)})
}

// CaptureRestartRecovery runs while the caller holds the stopped installation
// and data leases after verifying a new OS boot. The snapshot and its one-time
// enrollment commit together; retries never enroll attempts created later.
func (s *Store) CaptureRestartRecovery(ctx context.Context, evidence RestartEvidence) error {
	if evidence.ID == "" || evidence.DeviceID == "" || evidence.BootSession == "" || evidence.RestartFrom == "" || evidence.BootSession == evidence.RestartFrom || evidence.PlanSHA256 == "" {
		return errors.New("invalid restart recovery evidence")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing []byte
	err = tx.QueryRowContext(ctx, "SELECT data FROM objects WHERE bucket='execution_restart_evidence' AND id=?", evidence.ID).Scan(&existing)
	if err == nil {
		if string(existing) != string(protocol.JSON(evidence)) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT data FROM objects WHERE bucket='sessions' ORDER BY id")
	if err != nil {
		return err
	}
	sessions := []protocol.Session{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var session protocol.Session
		if err = json.Unmarshal(raw, &session); err != nil {
			rows.Close()
			return err
		}
		if session.DeviceID != evidence.DeviceID {
			rows.Close()
			return errors.New("restart recovery data belongs to another device")
		}
		sessions = append(sessions, session)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, session := range sessions {
		// A process born after the verified reboot is outside this certificate,
		// even when a partial old marker switch is being resumed.
		var raw []byte
		err = tx.QueryRowContext(ctx, "SELECT data FROM objects WHERE bucket='execution_origins' AND id=?", session.LastStartID).Scan(&raw)
		if err == nil {
			var origin executionOrigin
			if err = json.Unmarshal(raw, &origin); err != nil {
				return err
			}
			if origin.BootSession == evidence.BootSession {
				continue
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		attempt := executionAttempt(session)
		candidate := executionCandidate{ID: Hash(evidence.ID + ":" + attempt), EvidenceID: evidence.ID, DeviceID: session.DeviceID, SessionID: session.ID, Attempt: attempt}
		if _, err = tx.ExecContext(ctx, "INSERT INTO objects VALUES('execution_recovery_candidates',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", session.ID, protocol.JSON(candidate)); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO objects VALUES('execution_restart_evidence',?,?)", evidence.ID, protocol.JSON(evidence)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ExecutionRecovery(session protocol.Session) (*protocol.ExecutionRecovery, error) {
	var retired executionCandidate
	if err := s.Get("execution_retirements", session.ID, &retired); err == nil {
		if retired.DeviceID != session.DeviceID || retired.SessionID != session.ID || retired.Attempt != executionAttempt(session) || retired.ID != Hash(retired.EvidenceID+":"+retired.Attempt) {
			return nil, errors.New("retired execution identity changed")
		}
		return &protocol.ExecutionRecovery{ID: retired.ID, State: "retired"}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	var candidate executionCandidate
	if err := s.Get("execution_recovery_candidates", session.ID, &candidate); errors.Is(err, ErrNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if candidate.DeviceID != session.DeviceID || candidate.SessionID != session.ID || candidate.Attempt != executionAttempt(session) {
		return nil, nil
	}
	var evidence RestartEvidence
	if err := s.Get("execution_restart_evidence", candidate.EvidenceID, &evidence); err != nil {
		return nil, err
	}
	if evidence.DeviceID != session.DeviceID || candidate.ID != Hash(evidence.ID+":"+candidate.Attempt) {
		return nil, errors.New("execution recovery evidence changed")
	}
	return &protocol.ExecutionRecovery{ID: candidate.ID, State: "eligible"}, nil
}

// RetireExecution releases a local project claim without resolving an old
// model outcome. Session, audit and outbox commit together; original receipts,
// queued input and turn identifiers are preserved byte-for-byte.
func (s *Store) RetireExecution(session protocol.Session, command protocol.Command) (protocol.Session, protocol.Event, error) {
	id := command.RequestID
	if command.ID == "" || command.SourceID == "" || command.Kind != "execution.retire" || command.DeviceID != session.DeviceID || command.SessionID != session.ID {
		return session, protocol.Event{}, errors.New("invalid execution retirement command")
	}
	receipt := retirementReceipt{DeviceID: session.DeviceID, SessionID: session.ID, SourceID: command.SourceID, ReviewID: id, Attempt: executionAttempt(session)}
	view, err := s.ExecutionRecovery(session)
	if err != nil || view == nil || view.ID != id {
		return session, protocol.Event{}, errors.New("execution recovery review changed")
	}
	if view.State == "retired" {
		return session, protocol.Event{}, s.Put("execution_retirement_receipts", command.ID, receipt)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return session, protocol.Event{}, err
	}
	defer tx.Rollback()
	var raw []byte
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='sessions' AND id=?", session.ID).Scan(&raw); err != nil {
		return session, protocol.Event{}, err
	}
	var current protocol.Session
	if err = json.Unmarshal(raw, &current); err != nil {
		return session, protocol.Event{}, err
	}
	if executionAttempt(current) != executionAttempt(session) {
		return session, protocol.Event{}, ErrConflict
	}
	var candidate executionCandidate
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='execution_recovery_candidates' AND id=?", session.ID).Scan(&raw); err != nil {
		return session, protocol.Event{}, err
	}
	if err = json.Unmarshal(raw, &candidate); err != nil || candidate.ID != id || candidate.Attempt != executionAttempt(current) {
		return session, protocol.Event{}, ErrConflict
	}
	current.Frozen = true
	current.ExecutionRecovery = &protocol.ExecutionRecovery{ID: id, State: "retired"}
	current.UpdatedAt = protocol.Now()
	if _, err = tx.Exec("INSERT INTO objects VALUES('execution_retirements',?,?)", session.ID, protocol.JSON(candidate)); err != nil {
		return session, protocol.Event{}, err
	}
	if _, err = tx.Exec("UPDATE objects SET data=? WHERE bucket='sessions' AND id=?", protocol.JSON(current), session.ID); err != nil {
		return session, protocol.Event{}, err
	}
	if _, err = tx.Exec("INSERT INTO objects VALUES('execution_retirement_receipts',?,?)", command.ID, protocol.JSON(receipt)); err != nil {
		return session, protocol.Event{}, err
	}
	event := protocol.Event{ID: protocol.ID(), DeviceID: current.DeviceID, SessionID: current.ID, Kind: "session.updated", Payload: protocol.JSON(current), CreatedAt: protocol.Now()}
	result, err := tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, event.DeviceID, event.SessionID, protocol.JSON(event))
	if err != nil {
		return session, event, err
	}
	event.Sequence, err = result.LastInsertId()
	if err != nil {
		return session, event, err
	}
	return current, event, tx.Commit()
}

func recoveredRetirement(tx *sql.Tx, c protocol.Command, session protocol.Session) (bool, error) {
	var raw []byte
	err := tx.QueryRow("SELECT data FROM objects WHERE bucket='execution_retirement_receipts' AND id=?", c.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var receipt retirementReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return false, err
	}
	if receipt.DeviceID != c.DeviceID || receipt.SessionID != c.SessionID || receipt.SourceID != c.SourceID || receipt.ReviewID != c.RequestID || receipt.Attempt != executionAttempt(session) {
		return false, nil
	}
	err = tx.QueryRow("SELECT data FROM objects WHERE bucket='execution_retirements' AND id=?", session.ID).Scan(&raw)
	if err != nil {
		return false, err
	}
	var retirement executionCandidate
	if err = json.Unmarshal(raw, &retirement); err != nil {
		return false, err
	}
	return retirement.ID == receipt.ReviewID && retirement.Attempt == receipt.Attempt, nil
}
