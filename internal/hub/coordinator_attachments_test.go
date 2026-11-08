package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mewdex/internal/attachments"
	"mewdex/internal/config"
	"mewdex/internal/protocol"
)

func TestCoordinatorUsesHubUploadsForConversationInput(t *testing.T) {
	t.Setenv("MEWDEX_HARNESS_RPC", "1")
	h, state, server, admin := testHub(t)
	h.config.DataDir = filepath.Join(t.TempDir(), "data")
	h.config.Engine = config.Engine{Executable: os.Args[0], Args: []string{"-test.run=^TestProjectConfigurationRPCFixture$", "--"}}
	assets, err := attachments.New(filepath.Join(h.config.DataDir, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	h.SetAttachments(assets)
	project, err := state.SaveHubProject(h.config.DeviceID, "hp_upload", protocol.HubProjectRequest{Name: "Uploads"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	coordinator, err := h.StartCoordinator(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	session := protocol.Session{ID: "upload-session", DeviceID: h.coordinatorID, ProjectID: project.ID, State: "idle", Frozen: true}
	if err = coordinator.store.Put("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"image", "file"} {
		t.Run(kind, func(t *testing.T) {
			body := "uploaded file"
			if kind == "image" {
				body = "\x89PNG\r\n\x1a\n"
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/attachments?name=input&deviceId="+h.coordinatorID+"&projectId="+project.ID, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+admin)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var uploaded protocol.Attachment
			if response.StatusCode != http.StatusCreated || json.NewDecoder(response.Body).Decode(&uploaded) != nil {
				t.Fatal("upload failed", response.StatusCode)
			}
			command := protocol.Command{ID: "uploaded-" + kind, DeviceID: session.DeviceID, SessionID: session.ID, Kind: "enqueue", Input: []protocol.Input{{Type: kind, AttachmentID: uploaded.ID}}}
			result := coordinator.Handle(ctx, command)
			if result.State != "completed" {
				t.Fatal("coordinator could not read the Hub upload", result.Error)
			}
			meta, err := assets.Metadata(uploaded.ID)
			if err != nil || meta.TargetDeviceID != session.DeviceID || meta.TargetProjectID != project.ID {
				t.Fatal("conversation input changed the upload target", meta, err)
			}
		})
	}
	foreign, err := assets.SaveTarget("foreign.txt", strings.NewReader("other project"), "client", session.DeviceID, "hp_other")
	if err != nil {
		t.Fatal(err)
	}
	result := coordinator.Handle(ctx, protocol.Command{ID: "foreign-input", DeviceID: session.DeviceID, SessionID: session.ID, Kind: "enqueue", Input: []protocol.Input{{Type: "file", AttachmentID: foreign.ID}}})
	if result.State != "failed" || !strings.Contains(result.Error, "execution device and project") {
		t.Fatal("coordinator accepted another project's attachment", result)
	}
}
