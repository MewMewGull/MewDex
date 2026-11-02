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
	if err != nil || c.SourceID != request.SourceID || c.DeviceID != request.DeviceID {
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
		if err != nil || actor.Role != "executor" || actor.DeviceID != c.DeviceID {
			return protocol.OutputFile{}, errors.New("executor authorization changed during file upload")
		}
	}
	output := protocol.OutputFile{ID: meta.ID, CommandID: c.ID, SessionID: session.ID, DeviceID: session.DeviceID, ProjectID: session.ProjectID, Path: path, Attachment: meta, CreatedAt: meta.CreatedAt}
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
	if err != nil || actor.Role != "executor" || actor.DeviceID != c.DeviceID {
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
