package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mewdex/internal/attachments"
	"mewdex/internal/config"
	"mewdex/internal/protocol"
	"mewdex/internal/transport"
)

func TestProjectFileReturnUsesOriginalInputScope(t *testing.T) {
	h, s, server, admin := testHub(t)
	assets, err := attachments.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.SetAttachments(assets)
	inputCode, err := s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"allowed"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	inputToken, _, err := s.Pair(inputCode, "original input", "")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(inputToken)
	if err != nil {
		t.Fatal(err)
	}
	session := protocol.Session{ID: "output-session", DeviceID: "node", ProjectID: "allowed"}
	if err = s.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	c := protocol.Command{ID: "file-copy", Kind: "file.export", DeviceID: session.DeviceID, SessionID: session.ID, SourceID: actor.ID, File: &protocol.FileRequest{Path: "report.txt"}, CreatedAt: protocol.Now()}
	if _, _, err = s.Claim(c); err != nil {
		t.Fatal(err)
	}
	code, err := s.Invite("executor", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	workerToken, _, err := s.Pair(code, "worker", session.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "node-token")
	if err = os.WriteFile(tokenFile, []byte(workerToken), 0600); err != nil {
		t.Fatal(err)
	}
	local, err := attachments.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := local.Import(protocol.Attachment{ID: attachments.ExportID(c.SourceID, c.ID), Name: "report.txt", OwnerID: c.SourceID, TargetDeviceID: c.DeviceID, TargetProjectID: session.ProjectID, CreatedAt: c.CreatedAt}, strings.NewReader("snapshot bytes"))
	if err != nil {
		t.Fatal(err)
	}
	publish := transport.FilePublisher(config.Config{HubURL: server.URL, NodeTokenFile: tokenFile})
	output, err := publish(context.Background(), c, "report.txt", meta, strings.NewReader("snapshot bytes"))
	if err != nil || output.SessionID != session.ID || output.Attachment.OwnerID != actor.ID {
		t.Fatal(output, err)
	}
	if _, err = publish(context.Background(), c, "report.txt", meta, strings.NewReader("snapshot bytes")); err != nil {
		t.Fatal("same snapshot was not idempotent", err)
	}
	response := request(t, server.URL, admin, "GET", "/v1/file-exports?sessionId="+session.ID, nil)
	var outputs []protocol.OutputFile
	if err = json.NewDecoder(response.Body).Decode(&outputs); err != nil || len(outputs) != 1 {
		t.Fatal(outputs, err)
	}
	content := request(t, server.URL, admin, "GET", "/v1/attachments/"+meta.ID+"/content", nil)
	bytes, err := io.ReadAll(content.Body)
	if err != nil || string(bytes) != "snapshot bytes" {
		t.Fatal(string(bytes), err)
	}
	code, err = s.InviteWithAccess("client", protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"private"}}}}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := s.Pair(code, "restricted", "")
	if err != nil {
		t.Fatal(err)
	}
	if response = request(t, server.URL, other, "GET", "/v1/file-exports?sessionId="+session.ID, nil); response.StatusCode != http.StatusNotFound {
		t.Fatal("outside project read output list", response.StatusCode)
	}
	if response = request(t, server.URL, other, "GET", "/v1/attachments/"+meta.ID+"/content", nil); response.StatusCode != http.StatusNotFound {
		t.Fatal("outside project read output bytes", response.StatusCode)
	}
	if response = request(t, server.URL, workerToken, "GET", "/v1/file-exports?sessionId="+session.ID, nil); response.StatusCode != http.StatusForbidden {
		t.Fatal("executor accessed input output API", response.StatusCode)
	}
	if response = request(t, server.URL, admin, "POST", "/v1/file-exports?commandId="+c.ID, nil); response.StatusCode != http.StatusForbidden {
		t.Fatal("client could forge an executor upload", response.StatusCode)
	}
	posts := 0
	lost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		recorder := httptest.NewRecorder()
		h.Handler().ServeHTTP(recorder, r)
		if recorder.Code != http.StatusCreated {
			t.Errorf("snapshot was not committed before connection loss: %d", recorder.Code)
		}
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}))
	defer lost.Close()
	c.ID, c.CreatedAt = "copy-response-lost", protocol.Now()
	if _, _, err = s.Claim(c); err != nil {
		t.Fatal(err)
	}
	meta.ID, meta.CreatedAt = attachments.ExportID(c.SourceID, c.ID), c.CreatedAt
	deliver := transport.FilePublisher(config.Config{HubURL: lost.URL, NodeTokenFile: tokenFile})
	_, err = deliver(context.Background(), c, "report.txt", meta, strings.NewReader("snapshot bytes"))
	var uncertain *attachments.UnconfirmedDelivery
	if !errors.As(err, &uncertain) || posts != 1 {
		t.Fatal("lost response did not remain unconfirmed or request was replayed", err, posts)
	}
	stored, err := s.Outputs(session.ID)
	if err != nil || len(stored) != 2 {
		t.Fatal("committed snapshot was lost with its response", stored, err)
	}
	if err = s.UpdateAccess(actor.ID, protocol.Access{Targets: []protocol.TargetAccess{{DeviceID: "node", ProjectIDs: []string{"private"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = publish(context.Background(), c, "report.txt", meta, strings.NewReader("snapshot bytes")); err == nil {
		t.Fatal("revoked input scope still accepted file upload")
	}
}
