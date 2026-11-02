package executor

import (
	"context"
	"errors"
	"io"

	"mewdex/internal/attachments"
	"mewdex/internal/projectfiles"
	"mewdex/internal/protocol"
)

type FilePublisher func(context.Context, protocol.Command, string, protocol.Attachment, io.Reader) (protocol.OutputFile, error)

func (x *Executor) SetFilePublisher(publish FilePublisher) { x.filePublisher = publish }

func (x *Executor) fileCommand(ctx context.Context, c protocol.Command) protocol.CommandResult {
	if c.File == nil || c.SessionID == "" || c.SourceID == "" {
		return commandFailure(errors.New("file operations require a conversation and file request"))
	}
	var session protocol.Session
	if err := x.store.Get("sessions", c.SessionID, &session); err != nil || session.DeviceID != c.DeviceID {
		return commandFailure(errors.New("file conversation belongs to another device or is unavailable"))
	}
	// These are current project files. Reading them does not resume, retire or
	// alter an old execution, even when its original scope has changed.
	session.ExecutionScopeID = ""
	scope, err := x.executionScope(session)
	if err != nil {
		return commandFailure(err)
	}
	root := projectfiles.New(scope.Workspace(), scope.PrivatePaths())
	if c.Kind == "file.list" {
		page, err := root.List(ctx, c.File.Path, c.File.After)
		if err != nil {
			return commandFailure(err)
		}
		return protocol.CommandResult{State: "completed", Data: protocol.JSON(page)}
	}
	if c.Kind != "file.export" {
		return commandFailure(errors.New("unsupported file operation"))
	}
	if x.filePublisher == nil {
		return commandFailure(errors.New("this executor has no file return connection"))
	}
	file, relative, err := root.Open(c.File.Path)
	if err != nil {
		return commandFailure(err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return commandFailure(err)
	}
	if before.Size() > attachments.MaxSize {
		return commandFailure(errors.New("file exceeds the 20 MiB export limit"))
	}
	created := c.CreatedAt
	if created == "" {
		created = protocol.Now()
	}
	meta := protocol.Attachment{ID: attachments.ExportID(c.SourceID, c.ID), Name: before.Name(), CreatedAt: created, OwnerID: c.SourceID, TargetDeviceID: session.DeviceID, TargetProjectID: session.ProjectID}
	meta, err = x.attachments.Import(meta, fileContextReader{ctx, file})
	if err != nil {
		return commandFailure(err)
	}
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return commandFailure(errors.New("file changed while being copied; request a new snapshot when it is ready"))
	}
	copy, err := x.attachments.Open(meta.ID)
	if err != nil {
		return commandFailure(err)
	}
	defer copy.Close()
	output, err := x.filePublisher(ctx, c, relative, meta, copy)
	if err != nil {
		var uncertain *attachments.UnconfirmedDelivery
		if errors.As(err, &uncertain) {
			return protocol.CommandResult{State: "unknown", Error: err.Error()}
		}
		return commandFailure(err)
	}
	return protocol.CommandResult{State: "completed", Data: protocol.JSON(output)}
}

type fileContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r fileContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
