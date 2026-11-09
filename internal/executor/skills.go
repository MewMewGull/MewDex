package executor

import (
	"context"
	"errors"
	"path/filepath"

	"mewdex/internal/protocol"
)

func (x *Executor) skillList(ctx context.Context, c protocol.Command) protocol.CommandResult {
	projectID := c.ProjectID
	if c.SessionID != "" {
		var session protocol.Session
		if err := x.store.Get("sessions", c.SessionID, &session); err != nil {
			return commandFailure(err)
		}
		if session.DeviceID != c.DeviceID || projectID != "" && projectID != session.ProjectID {
			return commandFailure(errors.New("skill catalog target does not match the conversation"))
		}
		projectID = session.ProjectID
	}
	project, err := x.project(projectID)
	if err != nil {
		return commandFailure(err)
	}
	var response struct {
		Data []struct {
			Cwd    string `json:"cwd"`
			Skills []struct {
				protocol.SkillOption
				Enabled bool `json:"enabled"`
			} `json:"skills"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"data"`
	}
	if err = x.engine.Call(ctx, "skills/list", map[string]any{"cwds": []string{project.Path}, "forceReload": true}, &response); err != nil {
		return commandFailure(err)
	}
	catalog := protocol.SkillCatalog{Skills: []protocol.SkillOption{}, Errors: []string{}}
	for _, entry := range response.Data {
		if filepath.Clean(entry.Cwd) != filepath.Clean(project.Path) {
			continue
		}
		for _, skill := range entry.Skills {
			if skill.Enabled && skill.Name != "" && skill.Path != "" {
				catalog.Skills = append(catalog.Skills, skill.SkillOption)
			}
		}
		for _, issue := range entry.Errors {
			catalog.Errors = append(catalog.Errors, issue.Message)
		}
	}
	return protocol.CommandResult{State: "completed", Data: protocol.JSON(catalog)}
}
