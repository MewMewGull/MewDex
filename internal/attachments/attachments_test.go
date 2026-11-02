package attachments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mewdex/internal/protocol"
)

func TestCachedAttachmentRetainsItsOwnerAndExecutionTarget(t *testing.T) {
	assets, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := assets.SaveTarget("input.txt", strings.NewReader("retained input"), "input-a", "device-a", "project-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assets.Import(meta, strings.NewReader("retained input")); err != nil {
		t.Fatal("identical delivery cannot be retried", err)
	}
	for _, field := range []string{"owner", "device", "project", "name", "content"} {
		changed, content := meta, "retained input"
		switch field {
		case "owner":
			changed.OwnerID = "input-b"
		case "device":
			changed.TargetDeviceID = "device-b"
		case "project":
			changed.TargetProjectID = "project-b"
		case "name":
			changed.Name = "different.txt"
		case "content":
			changed.SHA256 = ""
			content = "replacement"
		}
		if _, err := assets.Import(changed, strings.NewReader(content)); err == nil {
			t.Fatalf("%s changed an existing attachment", field)
		}
	}
	for _, target := range [][2]string{{"device-b", "project-a"}, {"device-a", "project-b"}, {"", "project-a"}, {"device-a", ""}} {
		if _, err := assets.MetadataForTarget(meta.ID, target[0], target[1]); err == nil {
			t.Fatalf("accepted target %v", target)
		}
	}
	actual, err := assets.MetadataForTarget(meta.ID, "device-a", "project-a")
	if err != nil || actual != meta {
		t.Fatal("original binding changed", err)
	}
	path, _ := assets.Path(meta.ID)
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "retained input" {
		t.Fatal("original input changed", err)
	}
}

func TestInterruptedAttachmentAndMismatchedMetadataArePreserved(t *testing.T) {
	assets, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := protocol.ID()
	path, _ := assets.Path(id)
	if err = os.WriteFile(path, []byte("unknown previous import"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = assets.Import(protocol.Attachment{ID: id, Name: "new.txt", TargetDeviceID: "device", TargetProjectID: "project"}, strings.NewReader("new input")); err == nil {
		t.Fatal("overwrote an uncommitted input")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "unknown previous import" {
		t.Fatal("lost the uncommitted input", err)
	}
	if err = os.WriteFile(path+".json", protocol.JSON(protocol.Attachment{ID: protocol.ID()}), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = assets.Metadata(id); err == nil {
		t.Fatal("accepted another input's metadata")
	}
	if _, err = os.Stat(filepath.Join(assets.dir, id)); err != nil {
		t.Fatal(err)
	}
}
