package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"mewdex/internal/projectharness"
	"mewdex/internal/protocol"
)

func projectHarnessState(tx *sql.Tx, projectID string, revision int64) (protocol.ProjectHarnessState, error) {
	value := protocol.ProjectHarnessState{ProjectID: projectID, Configuration: projectharness.Empty()}
	var raw []byte
	if err := tx.QueryRow("SELECT data FROM objects WHERE bucket='hub_projects' AND id=?", projectID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return value, err
	}
	var project protocol.Project
	if err := json.Unmarshal(raw, &project); err != nil {
		return value, err
	}
	value.ProjectRevision = project.Revision
	bucket, key := "project_harness_current", projectID
	if revision > 0 {
		bucket, key = "project_harness_revisions", projectHarnessRevisionKey(projectID, revision)
	}
	err := tx.QueryRow("SELECT data FROM objects WHERE bucket=? AND id=?", bucket, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) && revision == 0 {
		value.Version = projectharness.Version(value.Configuration)
		return value, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	var saved protocol.ProjectHarnessRevision
	if err = json.Unmarshal(raw, &saved); err != nil {
		return value, err
	}
	if saved.ProjectID != projectID || saved.Revision <= 0 || len(saved.Version) != 64 {
		return value, errors.New("invalid project configuration revision")
	}
	value.Revision, value.Version, value.CreatedAt = saved.Revision, saved.Version, saved.CreatedAt
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='project_harness_versions' AND id=?", projectID+":"+saved.Version).Scan(&raw); err != nil {
		return value, err
	}
	if err = json.Unmarshal(raw, &value.Configuration); err != nil {
		return value, err
	}
	if projectharness.Version(value.Configuration) != saved.Version {
		return value, errors.New("project configuration failed its version check")
	}
	return value, nil
}

func projectHarnessRevisionKey(projectID string, revision int64) string {
	return fmt.Sprintf("%s:%020d", projectID, revision)
}

func (s *Store) ProjectHarness(projectID string, revision int64) (protocol.ProjectHarnessState, error) {
	if revision < 0 {
		return protocol.ProjectHarnessState{}, errors.New("invalid project configuration revision")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return protocol.ProjectHarnessState{}, err
	}
	defer tx.Rollback()
	return projectHarnessState(tx, projectID, revision)
}

// SaveProjectHarness commits the immutable content, ordered activation history,
// current pointer and scoped invalidation event together. Reverting content
// creates a new revision while retaining the original content hash.
func (s *Store) SaveProjectHarness(hubID, projectID string, input protocol.ProjectHarnessUpdate) (protocol.ProjectHarnessState, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return protocol.ProjectHarnessState{}, err
	}
	defer tx.Rollback()
	current, err := projectHarnessState(tx, projectID, 0)
	if err != nil {
		return current, err
	}
	if current.ProjectRevision != input.ExpectedProjectRevision {
		return current, ErrConflict
	}
	var raw []byte
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='hub_projects' AND id=?", projectID).Scan(&raw); err != nil {
		return current, err
	}
	var project protocol.Project
	if err = json.Unmarshal(raw, &project); err != nil {
		return current, err
	}
	configuration, err := projectharness.Normalize(input.Configuration, project.Directories)
	if err != nil {
		return current, err
	}
	version := projectharness.Version(configuration)
	if version == current.Version && (input.ExpectedRevision == current.Revision || input.ExpectedRevision+1 == current.Revision) {
		return current, nil
	}
	if input.ExpectedRevision != current.Revision {
		return current, ErrConflict
	}
	content := protocol.JSON(configuration)
	key := projectID + ":" + version
	if _, err = tx.Exec("INSERT INTO objects VALUES('project_harness_versions',?,?) ON CONFLICT(bucket,id) DO NOTHING", key, content); err != nil {
		return current, err
	}
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='project_harness_versions' AND id=?", key).Scan(&raw); err != nil {
		return current, err
	}
	if string(raw) != string(content) {
		return current, errors.New("project configuration version is immutable")
	}
	current.Revision++
	current.Version, current.CreatedAt, current.Configuration = version, protocol.Now(), configuration
	activation := protocol.ProjectHarnessRevision{ProjectID: projectID, Revision: current.Revision, Version: version, CreatedAt: current.CreatedAt}
	if _, err = tx.Exec("INSERT INTO objects VALUES('project_harness_revisions',?,?)", projectHarnessRevisionKey(projectID, current.Revision), protocol.JSON(activation)); err != nil {
		return current, err
	}
	if _, err = tx.Exec("INSERT INTO objects VALUES('project_harness_current',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", projectID, protocol.JSON(activation)); err != nil {
		return current, err
	}
	event := protocol.Event{ID: protocol.ID(), DeviceID: hubID, Kind: "project.harness.updated", Payload: protocol.JSON(activation), CreatedAt: activation.CreatedAt}
	if _, err = tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, hubID, "", protocol.JSON(event)); err != nil {
		return current, err
	}
	return current, tx.Commit()
}

func (s *Store) ProjectHarnessHistory(projectID string, before int64) (protocol.ProjectHarnessHistory, error) {
	page := protocol.ProjectHarnessHistory{Items: []protocol.ProjectHarnessRevision{}}
	if before < 0 {
		return page, errors.New("invalid project configuration history cursor")
	}
	key := projectHarnessRevisionKey(projectID, before)
	rows, err := s.db.Query("SELECT data FROM objects WHERE bucket='project_harness_revisions' AND json_extract(data,'$.projectId')=? AND (?=0 OR id<?) ORDER BY id DESC LIMIT 21", projectID, before, key)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var value protocol.ProjectHarnessRevision
		if err = rows.Scan(&raw); err != nil {
			return page, err
		}
		if err = json.Unmarshal(raw, &value); err != nil {
			return page, err
		}
		page.Items = append(page.Items, value)
	}
	if len(page.Items) > 20 {
		page.Items = page.Items[:20]
		page.NextBefore = page.Items[19].Revision
	}
	return page, rows.Err()
}
