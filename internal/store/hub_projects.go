package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"mewdex/internal/projectharness"
	"mewdex/internal/protocol"
)

var hubProjectID = regexp.MustCompile(`^hp_[a-zA-Z0-9_-]{1,80}$`)

func (s *Store) HubProjects() ([]protocol.Project, error) {
	return List[protocol.Project](s, "hub_projects")
}

// Called only on a verified, stopped restore/upgrade copy. Hub conversation
// workspaces are internal scratch directories and are rebuilt at the new root.
func (s *Store) ResetCoordinatorWorkspaces() error {
	_, err := s.db.Exec("DELETE FROM objects WHERE bucket IN ('workspaces','project_bindings')")
	return err
}

// Directory reports cannot rename or replace a Hub-owned project. Validate all
// references and commit the group and its invalidation event in one transaction.
func (s *Store) SaveHubProject(hubID, id string, input protocol.HubProjectRequest) (protocol.Project, error) {
	project := protocol.Project{ID: id, Name: strings.TrimSpace(input.Name), Category: strings.TrimSpace(input.Category), Directories: []protocol.ProjectDirectory{}}
	if !hubProjectID.MatchString(id) || project.Name == "" || utf8.RuneCountInString(project.Name) > 100 || utf8.RuneCountInString(project.Category) > 80 || len(input.Directories) > 100 {
		return project, errors.New("请输入有效的项目名称、分类和目录组")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return project, err
	}
	defer tx.Rollback()
	var raw []byte
	var prior protocol.Project
	err = tx.QueryRow("SELECT data FROM objects WHERE bucket='hub_projects' AND id=?", id).Scan(&raw)
	if err == nil {
		if err = json.Unmarshal(raw, &prior); err != nil {
			return project, err
		}
		if prior.Removed {
			return project, ErrNotFound
		}
		project.Revision = prior.Revision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return project, err
	}
	seen := map[string]bool{}
	for _, directory := range input.Directories {
		key := projectMetadataKey(directory.DeviceID, directory.ProjectID)
		if seen[key] {
			return project, errors.New("同一目录不能重复添加")
		}
		seen[key] = true
		if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='devices' AND id=?", directory.DeviceID).Scan(&raw); err != nil {
			return project, errors.New("目录所属的执行端尚未在 Hub 注册")
		}
		var device protocol.Device
		if err = json.Unmarshal(raw, &device); err != nil {
			return project, err
		}
		found := false
		for _, binding := range device.Projects {
			if binding.ID == directory.ProjectID && len(binding.Directories) == 0 && binding.Path != "" {
				if directory.Path != "" && directory.Path != binding.Path {
					return project, errors.New("执行端目录绑定已改变，请刷新后重试")
				}
				directory.Path = binding.Path
				if strings.TrimSpace(directory.Name) == "" {
					directory.Name = binding.Name
				}
				project.Directories = append(project.Directories, directory)
				found = true
				break
			}
		}
		if !found {
			return project, errors.New("执行端目录绑定已不可用")
		}
	}
	if prior.ID != "" {
		configuration, err := projectHarnessState(tx, id, 0)
		if err != nil {
			return project, err
		}
		if _, err = projectharness.Normalize(configuration.Configuration, project.Directories); err != nil {
			return project, errors.New("项目脚本仍引用这些目录，请先调整项目自动化配置")
		}
	}
	if project.Name == prior.Name && project.Category == prior.Category && string(protocol.JSON(project.Directories)) == string(protocol.JSON(prior.Directories)) && (input.ExpectedRevision == prior.Revision || input.ExpectedRevision+1 == prior.Revision) {
		return prior, nil
	}
	if project.Revision != input.ExpectedRevision {
		return project, ErrConflict
	}
	project.Revision++
	if _, err = tx.Exec("INSERT INTO objects VALUES('hub_projects',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", id, protocol.JSON(project)); err != nil {
		return project, err
	}
	event := protocol.Event{ID: protocol.ID(), DeviceID: hubID, Kind: "project.catalog.updated", Payload: protocol.JSON(map[string]string{"projectId": id}), CreatedAt: protocol.Now()}
	if _, err = tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, hubID, "", protocol.JSON(event)); err != nil {
		return project, err
	}
	return project, tx.Commit()
}

// Remove only the catalogue entry. Existing conversations retain the project,
// directory bindings and harness so their history and execution remain usable.
func (s *Store) RemoveHubProject(hubID, id string, expectedRevision int64) (protocol.Project, error) {
	var project protocol.Project
	if !hubProjectID.MatchString(id) {
		return project, ErrNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return project, err
	}
	defer tx.Rollback()
	var raw []byte
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='hub_projects' AND id=?", id).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return project, ErrNotFound
	} else if err != nil {
		return project, err
	}
	if err = json.Unmarshal(raw, &project); err != nil {
		return project, err
	}
	if project.Removed {
		return project, nil
	}
	if project.Revision != expectedRevision {
		return project, ErrConflict
	}
	project.Removed = true
	project.Revision++
	if _, err = tx.Exec("UPDATE objects SET data=? WHERE bucket='hub_projects' AND id=?", protocol.JSON(project), id); err != nil {
		return project, err
	}
	event := protocol.Event{ID: protocol.ID(), DeviceID: hubID, Kind: "project.catalog.updated", Payload: protocol.JSON(map[string]string{"projectId": id}), CreatedAt: protocol.Now()}
	if _, err = tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, hubID, "", protocol.JSON(event)); err != nil {
		return project, err
	}
	return project, tx.Commit()
}

// Import the old device-owned catalogue once. Equal names are deliberately kept
// as separate projects until the owner explicitly groups their directories.
func (s *Store) ImportHubProjects(hubID string) error {
	var initialized bool
	if err := s.Get("settings", "hub_projects_imported", &initialized); err == nil && initialized {
		return nil
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	devices, err := List[protocol.Device](s, "devices")
	if err != nil {
		return err
	}
	for _, device := range devices {
		for _, binding := range device.Projects {
			if binding.Path == "" || len(binding.Directories) != 0 {
				continue
			}
			hash := sha256.Sum256(protocol.JSON([]string{device.ID, binding.ID}))
			id := "hp_" + hex.EncodeToString(hash[:16])
			name := binding.Name
			if strings.TrimSpace(name) == "" {
				name = binding.ID
			}
			var existing protocol.Project
			if err = s.Get("hub_projects", id, &existing); err == nil {
				continue
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
			_, err = s.SaveHubProject(hubID, id, protocol.HubProjectRequest{Name: name, Category: binding.Category, Directories: []protocol.ProjectDirectory{{DeviceID: device.ID, ProjectID: binding.ID, Path: binding.Path, Name: name}}})
			if err != nil {
				return err
			}
		}
	}
	return s.Put("settings", "hub_projects_imported", true)
}
