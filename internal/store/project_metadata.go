package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"mewdex/internal/protocol"
	"strings"
)

type projectMetadata struct {
	DeviceID  string `json:"deviceId"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Category  string `json:"category"`
}

func projectMetadataKey(device, project string) string {
	return string(protocol.JSON([]string{device, project}))
}

// Physical project bindings are reported by executors. Names and categories
// edited on the Hub are authoritative across offline reads and stale reconnects.
func applyProjectMetadata(tx *sql.Tx, device protocol.Device) (protocol.Device, error) {
	device.Projects = append([]protocol.Project{}, device.Projects...)
	for i, project := range device.Projects {
		var raw []byte
		err := tx.QueryRow("SELECT data FROM objects WHERE bucket='project_metadata' AND id=?", projectMetadataKey(device.ID, project.ID)).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return device, err
		}
		var metadata projectMetadata
		if err = json.Unmarshal(raw, &metadata); err != nil {
			return device, err
		}
		device.Projects[i].Name, device.Projects[i].Category = metadata.Name, metadata.Category
	}
	return device, nil
}

func (s *Store) RegisterDevice(device protocol.Device) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	device, err = applyProjectMetadata(tx, device)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO objects VALUES('devices',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", device.ID, protocol.JSON(device)); err != nil {
		return err
	}
	return tx.Commit()
}

// The public catalogue, immutable path, update event and command receipt commit
// together. An offline rename never needs to start or contact its executor.
func (s *Store) UpdateProjectMetadata(c protocol.Command) (protocol.CommandResult, error) {
	result := protocol.CommandResult{CommandID: c.ID, State: "completed"}
	if c.SessionID != "" || c.Workspace == nil || c.ProjectID == "" {
		return result, errors.New("设备和项目编号及项目信息必填")
	}
	name, category := strings.TrimSpace(c.Workspace.Name), strings.TrimSpace(c.Workspace.Category)
	if name == "" || len(name) > 100 || len(category) > 80 {
		return result, errors.New("请输入有效的项目名称和分类")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var raw []byte
	if err = tx.QueryRow("SELECT data FROM objects WHERE bucket='devices' AND id=?", c.DeviceID).Scan(&raw); err != nil {
		return result, err
	}
	var device protocol.Device
	if err = json.Unmarshal(raw, &device); err != nil {
		return result, err
	}
	found := false
	for i, project := range device.Projects {
		if project.ID == c.ProjectID {
			device.Projects[i].Name, device.Projects[i].Category = name, category
			result.Data = protocol.JSON(device.Projects[i])
			found = true
			break
		}
	}
	if !found {
		return result, errors.New("项目已不可用，请刷新列表")
	}
	metadata := projectMetadata{DeviceID: c.DeviceID, ProjectID: c.ProjectID, Name: name, Category: category}
	if _, err = tx.Exec("INSERT INTO objects VALUES('project_metadata',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", projectMetadataKey(c.DeviceID, c.ProjectID), protocol.JSON(metadata)); err != nil {
		return result, err
	}
	if _, err = tx.Exec("UPDATE objects SET data=? WHERE bucket='devices' AND id=?", protocol.JSON(device), device.ID); err != nil {
		return result, err
	}
	event := protocol.Event{ID: protocol.ID(), DeviceID: c.DeviceID, Kind: "workspace.catalog.updated", Payload: protocol.JSON(device.Projects), CreatedAt: protocol.Now()}
	if _, err = tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", event.ID, event.DeviceID, "", protocol.JSON(event)); err != nil {
		return result, err
	}
	receipt := protocol.Event{ID: protocol.ID(), DeviceID: c.DeviceID, Kind: "command.result", Payload: protocol.JSON(result), CreatedAt: protocol.Now()}
	if _, err = tx.Exec("INSERT INTO events(id,device,session,data) VALUES(?,?,?,?)", receipt.ID, receipt.DeviceID, "", protocol.JSON(receipt)); err != nil {
		return result, err
	}
	if _, err = tx.Exec("UPDATE commands SET result=? WHERE id=?", protocol.JSON(result), c.ID); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
