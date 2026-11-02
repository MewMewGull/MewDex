package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"mewdex/internal/protocol"
)

// ValidateSnapshot checks an existing file without creating or migrating tables.
// It runs before a staged database is opened for restore preparation.
func ValidateSnapshot(ctx context.Context, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("backup database is missing or empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	var check string
	if err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil || check != "ok" {
		return errors.New("backup database failed its integrity check")
	}
	var version, count int
	if err = db.QueryRowContext(ctx, "SELECT min(version),count(*) FROM schema_version").Scan(&version, &count); err != nil || count != 1 || (version != 1 && version != 2) {
		return errors.New("unsupported backup database schema")
	}
	for _, query := range []string{"SELECT bucket,id,data FROM objects LIMIT 0", "SELECT sequence,id,device,session,data,acked FROM events LIMIT 0", "SELECT id,request,result FROM commands LIMIT 0", "SELECT hash,data FROM identities LIMIT 0", "SELECT hash,role,expires FROM invitations LIMIT 0"} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return errors.New("backup database is missing required state tables")
		}
		rows.Close()
	}
	return nil
}

// PrepareRestore modifies a staged copy only. Old permissions and callbacks must
// not become active when an earlier snapshot is adopted. Restored work is frozen.
func (s *Store) PrepareRestore(ctx context.Context, localDevice string, executorDB bool) error {
	return s.prepareStateCopy(ctx, localDevice, executorDB, true)
}

// PrepareUpgrade handles a fresh stopped executor copy, preserving current
// network identities. The caller must reject any source change before adoption.
func (s *Store) PrepareUpgrade(ctx context.Context, localDevice string) error {
	return s.prepareStateCopy(ctx, localDevice, true, false)
}

func (s *Store) prepareStateCopy(ctx context.Context, localDevice string, executorDB, revokeAccess bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT bucket,id,data FROM objects WHERE bucket IN ('sessions','queue_items','interactions','devices')")
	if err != nil {
		return err
	}
	type item struct {
		bucket, id string
		data       []byte
	}
	items := []item{}
	for rows.Next() {
		var value item
		if err = rows.Scan(&value.bucket, &value.id, &value.data); err != nil {
			rows.Close()
			return err
		}
		items = append(items, value)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, value := range items {
		var data any
		switch value.bucket {
		case "sessions":
			var session protocol.Session
			if json.Unmarshal(value.data, &session) != nil || session.ID != value.id || session.DeviceID == "" || (executorDB && session.DeviceID != localDevice) {
				return errors.New("restored session belongs to a different device or has an invalid identity")
			}
			session.Frozen = true
			if revokeAccess && session.ExecutionRecovery != nil && session.ExecutionRecovery.State != "retired" {
				session.ExecutionRecovery = nil
			}
			if executorDB && session.ExecutionScopeID != "" {
				// This is the already authorized, staged restore copy. Its paths
				// must be bound afresh after adoption, never reused from backup.
				if _, err = tx.ExecContext(ctx, "INSERT INTO objects(bucket,id,data) VALUES('execution_scope_restore',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", session.ID, protocol.JSON(map[string]string{"previousScopeId": session.ExecutionScopeID, "at": protocol.Now()})); err != nil {
					return err
				}
				session.ExecutionScopeID = ""
			}
			if session.ExecutionUnconfirmed() {
				session.State, session.StopStatus = "unknown", "unconfirmed"
			}
			data = session
		case "queue_items":
			var queued protocol.QueueItem
			if json.Unmarshal(value.data, &queued) != nil || queued.ID != value.id || queued.DeviceID == "" || (executorDB && queued.DeviceID != localDevice) {
				return errors.New("restored queue has an invalid device or item identity")
			}
			if queued.State == "dispatching" || queued.State == "running" {
				queued.State = "unknown"
				queued.Error = "restored task outcome is unconfirmed; input was not replayed"
				queued.Revision++
				queued.UpdatedAt = protocol.Now()
			}
			data = queued
		case "interactions":
			var interaction protocol.Interaction
			if json.Unmarshal(value.data, &interaction) != nil || interaction.ID != value.id {
				return errors.New("restored interaction has an invalid identity")
			}
			if interaction.State == "pending" || interaction.State == "responding" {
				interaction.State = "expired"
			}
			data = interaction
		case "devices":
			var device protocol.Device
			if json.Unmarshal(value.data, &device) != nil || device.ID != value.id {
				return errors.New("restored device has an invalid identity")
			}
			device.Online = false
			data = device
		}
		if _, err = tx.ExecContext(ctx, "UPDATE objects SET data=? WHERE bucket=? AND id=?", protocol.JSON(data), value.bucket, value.id); err != nil {
			return err
		}
	}
	// A snapshot can predate a revocation. Require fresh pairing rather than
	// silently reviving an old input, executor, or invitation credential.
	if revokeAccess {
		if _, err = tx.ExecContext(ctx, "DELETE FROM identities; DELETE FROM invitations; DELETE FROM objects WHERE bucket IN ('execution_recovery_candidates','execution_restart_evidence','execution_origins','execution_retirement_receipts');"); err != nil {
			return err
		}
	}
	if executorDB {
		if _, err = tx.ExecContext(ctx, `UPDATE events SET acked=1 WHERE json_extract(data,'$.kind') IN ('session.updated','interaction.updated','queue.item.updated')`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO objects(bucket,id,data) VALUES('maintenance','restore_pending',?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", protocol.JSON(map[string]bool{"pending": true})); err != nil {
			return err
		}
	}
	return tx.Commit()
}
