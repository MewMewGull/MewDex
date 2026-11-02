package executor

import (
	"context"
	"errors"

	"mewdex/internal/protocol"
)

func (x *Executor) modelList(ctx context.Context, c protocol.Command) protocol.CommandResult {
	projectID := c.ProjectID
	if c.SessionID != "" {
		var session protocol.Session
		if err := x.store.Get("sessions", c.SessionID, &session); err != nil {
			return commandFailure(err)
		}
		if session.DeviceID != c.DeviceID || projectID != "" && projectID != session.ProjectID {
			return commandFailure(errors.New("model catalog target does not match the conversation"))
		}
		projectID = session.ProjectID
	}
	if _, err := x.project(projectID); err != nil {
		return commandFailure(err)
	}
	request := protocol.ModelListRequest{Limit: 30}
	if c.Models != nil {
		request = *c.Models
		if request.Limit == 0 {
			request.Limit = 30
		}
	}
	if request.Limit < 1 || request.Limit > 100 {
		return commandFailure(errors.New("model page size must be between 1 and 100"))
	}
	var response struct {
		Data []struct {
			protocol.ModelOption
			Hidden bool `json:"hidden"`
		} `json:"data"`
		NextCursor string `json:"nextCursor"`
	}
	params := map[string]any{"limit": request.Limit, "includeHidden": false}
	if request.Cursor != "" {
		params["cursor"] = request.Cursor
	}
	if err := x.engine.Call(ctx, "model/list", params, &response); err != nil {
		return commandFailure(err)
	}
	page := protocol.ModelPage{Models: []protocol.ModelOption{}, NextCursor: response.NextCursor}
	for _, model := range response.Data {
		if !model.Hidden {
			page.Models = append(page.Models, model.ModelOption)
		}
	}
	return protocol.CommandResult{State: "completed", Data: protocol.JSON(page)}
}
