package executor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"mewdex/internal/attachments"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func TestProjectFileExportDoesNotRunOrReleaseUnconfirmedExecution(t *testing.T) {
	x, s, fake := setup(t)
	project := x.config.Projects[0].Path
	private := filepath.Join(project, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	fake.environment = &engine.ExecutionEnvironment{Private: []string{private}}
	created := x.Handle(context.Background(), protocol.Command{ID: "file-create", DeviceID: "device", ProjectID: "project", SourceID: "client", Kind: "create"})
	if created.State != "completed" || created.Session == nil {
		t.Fatalf("create: %+v", created)
	}
	session := *created.Session
	session.State, session.StopStatus, session.Frozen = "unknown", "unconfirmed", true
	if err := s.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	before := string(protocol.JSON(session))
	fake.mu.Lock()
	callsBefore := len(fake.methods)
	fake.mu.Unlock()
	if err := os.WriteFile(filepath.Join(project, "result.txt"), []byte("stable result"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "auth.json"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	published := 0
	x.SetFilePublisher(func(_ context.Context, c protocol.Command, path string, meta protocol.Attachment, body io.Reader) (protocol.OutputFile, error) {
		published++
		data, err := io.ReadAll(body)
		if err != nil || string(data) != "stable result" || path != "result.txt" {
			t.Fatal(string(data), path, err)
		}
		return protocol.OutputFile{ID: meta.ID, CommandID: c.ID, SessionID: c.SessionID, DeviceID: c.DeviceID, ProjectID: "project", Path: path, Attachment: meta, CreatedAt: meta.CreatedAt}, nil
	})
	command := protocol.Command{ID: "export-once", DeviceID: "device", SessionID: session.ID, SourceID: "client", Kind: "file.export", File: &protocol.FileRequest{Path: "result.txt"}, CreatedAt: protocol.Now()}
	result := x.Handle(context.Background(), command)
	if result.State != "completed" {
		t.Fatalf("export: %+v", result)
	}
	if again := x.Handle(context.Background(), command); again.State != "completed" || published != 1 {
		t.Fatal("export was repeated", again, published)
	}
	command.ID, command.File.Path = "export-private", "private/auth.json"
	if denied := x.Handle(context.Background(), command); denied.State != "failed" || published != 1 {
		t.Fatal("private bytes reached publisher", denied)
	}
	x.SetFilePublisher(func(context.Context, protocol.Command, string, protocol.Attachment, io.Reader) (protocol.OutputFile, error) {
		published++
		return protocol.OutputFile{}, &attachments.UnconfirmedDelivery{Cause: errors.New("lost receipt")}
	})
	command.ID, command.File.Path = "export-unknown", "result.txt"
	if uncertain := x.Handle(context.Background(), command); uncertain.State != "unknown" {
		t.Fatal(uncertain)
	}
	if again := x.Handle(context.Background(), command); again.State != "unknown" || published != 2 {
		t.Fatal("unknown export was repeated", again, published)
	}
	var after protocol.Session
	if err := s.Get("sessions", session.ID, &after); err != nil || string(protocol.JSON(after)) != before {
		t.Fatal("file export altered execution", after, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.methods) != callsBefore {
		t.Fatal("file operation invoked the engine", fake.methods[callsBefore:])
	}
}
