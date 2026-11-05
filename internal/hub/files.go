package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"mewdex/internal/attachments"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type fileExporterKey struct{}

func (h *Hub) projectFileCommand(ctx context.Context, command protocol.Command) protocol.CommandResult {
	fail := func(err error) protocol.CommandResult {
		return protocol.CommandResult{CommandID: command.ID, State: "failed", Error: err.Error()}
	}
	var session protocol.Session
	if command.File == nil || h.store.Get("sessions", command.SessionID, &session) != nil || session.DeviceID != h.coordinatorID {
		return fail(errors.New("Hub conversation and file request required"))
	}
	var project protocol.Project
	if err := h.store.Get("hub_projects", session.ProjectID, &project); err != nil {
		return fail(err)
	}
	index := 0
	if command.File.Directory != nil {
		index = *command.File.Directory
	}
	if index < 0 || index >= len(project.Directories) {
		return fail(errors.New("请选择项目目录组中的目录"))
	}
	directory := project.Directories[index]
	kind := "terminal.file.list"
	if command.Kind == "file.export" {
		kind = "terminal.file.export"
	}
	child := protocol.Command{ID: "f_" + command.ID, DeviceID: directory.DeviceID, ProjectID: directory.ProjectID, Kind: kind, File: command.File, RequestID: command.ID, SourceID: command.SourceID, CreatedAt: command.CreatedAt, Terminal: &protocol.TerminalRequest{OwnerID: session.ID}}
	result, err := h.runTerminal(ctx, child)
	result.CommandID = command.ID
	if err != nil {
		result.Error = err.Error()
		if result.State == "accepted" {
			result.State = "unknown"
		}
	}
	return result
}

func (h *Hub) fileExecutionDevice(command protocol.Command) string {
	if command.DeviceID != h.coordinatorID {
		return command.DeviceID
	}
	child, err := h.store.Command("f_" + command.ID)
	if err != nil || child.Kind != "terminal.file.export" || child.RequestID != command.ID || child.SourceID != command.SourceID || child.Terminal == nil || child.Terminal.OwnerID != command.SessionID {
		return ""
	}
	return child.DeviceID
}

func (h *Hub) exportCommand(id string) (protocol.Command, protocol.Session, error) {
	c, err := h.store.Command(id)
	if err != nil || c.Kind != "file.export" || c.File == nil || c.SessionID == "" {
		return c, protocol.Session{}, errors.New("file export request is unavailable")
	}
	var session protocol.Session
	if err = h.store.Get("sessions", c.SessionID, &session); err != nil || session.DeviceID != c.DeviceID {
		return c, session, errors.New("file export target is unavailable")
	}
	actor, err := h.store.Identity(c.SourceID)
	if err != nil || actor.Role == "executor" || !actor.Access.Allows(session.DeviceID, session.ProjectID) {
		return c, session, errors.New("file export input is no longer authorized")
	}
	return c, session, nil
}

// PublishFileExport is shared by local and remote executors. The original
// input command determines ownership; an upload cannot choose another scope.
func (h *Hub) PublishFileExport(ctx context.Context, request protocol.Command, path string, meta protocol.Attachment, body io.Reader) (protocol.OutputFile, error) {
	c, session, err := h.exportCommand(request.ID)
	if err != nil || c.SourceID != request.SourceID || request.DeviceID != c.DeviceID && request.DeviceID != h.fileExecutionDevice(c) {
		return protocol.OutputFile{}, errors.New("file export does not match its original request")
	}
	if h.attachments == nil {
		return protocol.OutputFile{}, errors.New("file return storage is unavailable")
	}
	if meta.ID != attachments.ExportID(c.SourceID, c.ID) || meta.Name == "" || len(meta.SHA256) != 64 || meta.Size < 0 || meta.Size > attachments.MaxSize {
		return protocol.OutputFile{}, errors.New("invalid exported file metadata")
	}
	meta.OwnerID, meta.TargetDeviceID, meta.TargetProjectID, meta.CreatedAt = c.SourceID, session.DeviceID, session.ProjectID, c.CreatedAt
	meta, err = h.attachments.Import(meta, body)
	if err != nil {
		return protocol.OutputFile{}, err
	}
	if err = ctx.Err(); err != nil {
		return protocol.OutputFile{}, err
	}
	if _, _, err = h.exportCommand(c.ID); err != nil {
		return protocol.OutputFile{}, err
	}
	if exporter, ok := ctx.Value(fileExporterKey{}).(string); ok {
		actor, err := h.store.Identity(exporter)
		if err != nil || actor.Role != "executor" || actor.DeviceID != h.fileExecutionDevice(c) {
			return protocol.OutputFile{}, errors.New("executor authorization changed during file upload")
		}
	}
	output := protocol.OutputFile{ID: meta.ID, CommandID: c.ID, SessionID: session.ID, DeviceID: session.DeviceID, ProjectID: session.ProjectID, Path: path, Attachment: meta, CreatedAt: meta.CreatedAt}
	if c.DeviceID == h.coordinatorID {
		child, err := h.store.Command("f_" + c.ID)
		if err != nil {
			return output, err
		}
		var device protocol.Device
		if err = h.store.Get("devices", child.DeviceID, &device); err != nil {
			return output, err
		}
		for _, binding := range device.Projects {
			if binding.ID == child.ProjectID {
				output.Directory = &protocol.ProjectDirectory{DeviceID: device.ID, ProjectID: binding.ID, Name: binding.Name, Path: binding.Path}
				break
			}
		}
	}
	if err = h.store.RecordOutput(output); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return output, err
		}
		return output, &attachments.UnconfirmedDelivery{Cause: err}
	}
	h.notify()
	return output, nil
}

func (h *Hub) receiveFileExport(w http.ResponseWriter, r *http.Request) {
	actor := identity(r)
	c, _, err := h.exportCommand(r.URL.Query().Get("commandId"))
	if err != nil || actor.Role != "executor" || actor.DeviceID != h.fileExecutionDevice(c) {
		problem(w, 403, errors.New("an authorized executor export request is required"))
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Mewdex-Output"))
	var request protocol.FileExportRequest
	if err != nil || len(raw) > 16<<10 || json.Unmarshal(raw, &request) != nil {
		problem(w, 400, errors.New("invalid file snapshot metadata"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, attachments.MaxSize+1)
	defer r.Body.Close()
	output, err := h.PublishFileExport(context.WithValue(r.Context(), fileExporterKey{}, actor.ID), c, request.Path, request.Attachment, r.Body)
	if err != nil {
		var uncertain *attachments.UnconfirmedDelivery
		if errors.As(err, &uncertain) {
			problem(w, http.StatusInternalServerError, err)
			return
		}
		problem(w, 400, err)
		return
	}
	reply(w, 201, output)
}

func (h *Hub) outputs(w http.ResponseWriter, r *http.Request) {
	var session protocol.Session
	if h.store.Get("sessions", r.URL.Query().Get("sessionId"), &session) != nil || !identity(r).Access.Allows(session.DeviceID, session.ProjectID) {
		problem(w, 404, errors.New("conversation not found"))
		return
	}
	items, err := h.store.Outputs(session.ID)
	if err != nil {
		problem(w, 500, err)
		return
	}
	reply(w, 200, items)
}
