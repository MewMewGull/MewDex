package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

const testPassword = "mewdex device backup test passphrase"

func TestEncryptedArchiveRoundTripAndExistingFilesArePreserved(t *testing.T) {
	root, output := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("conversation and policy state\n"), 10000)
	if err := os.WriteFile(filepath.Join(root, "data", "hub.db"), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "control.json"), []byte("ephemeral control secret"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(output, "device.mewdex-backup")
	manifest, err := Create(context.Background(), root, archive, testPassword, json.RawMessage(`{"deviceId":"test-device"}`), []Selection{{Path: "data", Name: "data"}}, Options{Exclude: func(name string) bool { return name == "data/control.json" }})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Name != "data/hub.db" {
		t.Fatal("excluded state entered the backup")
	}
	encrypted, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("test-device")) || bytes.Contains(encrypted, []byte("conversation and policy state")) {
		t.Fatal("backup contains unencrypted application state")
	}
	if _, err = Create(context.Background(), root, archive, testPassword, json.RawMessage(`{}`), nil, Options{}); err == nil {
		t.Fatal("existing backup was overwritten")
	}
	unchanged, _ := os.ReadFile(archive)
	if !bytes.Equal(unchanged, encrypted) {
		t.Fatal("existing backup bytes changed")
	}
	restored, err := Extract(context.Background(), archive, testPassword, output, "restored", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Metadata, manifest.Metadata) {
		t.Fatal("backup metadata changed")
	}
	actual, err := os.ReadFile(filepath.Join(output, "restored", "data", "hub.db"))
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatal("restored state does not match its source")
	}
	if _, err = os.Stat(filepath.Join(output, "restored", "data", "control.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ephemeral control state was restored")
	}
	for name, data := range map[string][]byte{"wrong-password": encrypted, "truncated": encrypted[:len(encrypted)-1], "modified": append([]byte{}, encrypted...)} {
		if name == "modified" {
			data[len(data)-4] ^= 0x80
		}
		file := filepath.Join(output, name+".age")
		if err = os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		password := testPassword
		if name == "wrong-password" {
			password = "a different backup passphrase"
		}
		if _, err = Extract(context.Background(), file, password, output, name, Limits{}); err == nil {
			t.Fatalf("%s backup was accepted", name)
		}
		if _, statErr := os.Stat(filepath.Join(output, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("%s left a partially restored directory", name)
		}
	}
	if _, err = Extract(context.Background(), archive, testPassword, output, "restored", Limits{}); err == nil {
		t.Fatal("existing restored data was replaced")
	}
}

func fixtureArchive(t *testing.T, manifest Manifest, headers []*tar.Header, contents [][]byte, trailing []byte) string {
	t.Helper()
	var raw bytes.Buffer
	recipient, err := age.NewScryptRecipient(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	recipient.SetWorkFactor(1) // Test malformed archives without repeated costly KDFs.
	encrypted, err := age.Encrypt(&raw, recipient)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(encrypted)
	metadata, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(metadata)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(metadata); err != nil {
		t.Fatal(err)
	}
	for i, header := range headers {
		if err = writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err = writer.Write(contents[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = encrypted.Write(trailing); err != nil {
		t.Fatal(err)
	}
	if err = encrypted.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "fixture.age")
	if err = os.WriteFile(archive, raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestRestoreRejectsUnsafePathsAndUnverifiedArchiveContents(t *testing.T) {
	emptyHash := sha256.Sum256(nil)
	digest := hex.EncodeToString(emptyHash[:])
	base := Manifest{Format: 1, Kind: "mewdex-device", Metadata: json.RawMessage(`{}`)}
	for _, name := range []string{"../outside", "/absolute", `data\outside`, "data/CON.txt", "data/file:stream", "data/ending.", "data/../outside", "data/COM1", "data/CONIN$", "data/LPT²", "data/file "} {
		t.Run(name, func(t *testing.T) {
			manifest := base
			manifest.Files = []File{{Name: name, SHA256: digest}}
			archive := fixtureArchive(t, manifest, nil, nil, nil)
			parent := t.TempDir()
			if _, err := Extract(context.Background(), archive, testPassword, parent, "new-state", Limits{}); err == nil {
				t.Fatal("unsafe path was accepted")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatal("unsafe archive changed its restore parent")
			}
		})
	}
	cases := []struct {
		name     string
		files    []File
		headers  []*tar.Header
		contents [][]byte
		trailing []byte
		limits   Limits
	}{
		{name: "case-collision", files: []File{{Name: "data/A", SHA256: digest}, {Name: "data/a", SHA256: digest}}},
		{name: "file-directory-collision", files: []File{{Name: "data", SHA256: digest}, {Name: "data/child", SHA256: digest}}},
		{name: "symbolic-link", files: []File{{Name: "data/file", SHA256: digest}}, headers: []*tar.Header{{Name: "data/file", Typeflag: tar.TypeSymlink, Linkname: "../../outside"}}, contents: [][]byte{nil}},
		{name: "extra-file", headers: []*tar.Header{{Name: "undeclared", Typeflag: tar.TypeReg}}, contents: [][]byte{nil}},
		{name: "digest-mismatch", files: []File{{Name: "data/file", Bytes: 1, SHA256: digest}}, headers: []*tar.Header{{Name: "data/file", Typeflag: tar.TypeReg, Size: 1}}, contents: [][]byte{[]byte("x")}},
		{name: "trailing-content", trailing: []byte("not declared in the tar archive")},
		{name: "size-limit", files: []File{{Name: "data/file", Bytes: 2, SHA256: digest}}, limits: Limits{MaxBytes: 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := base
			manifest.Files = test.files
			archive := fixtureArchive(t, manifest, test.headers, test.contents, test.trailing)
			parent := t.TempDir()
			if _, err := Extract(context.Background(), archive, testPassword, parent, "new-state", test.limits); err == nil {
				t.Fatal("invalid backup contents were accepted")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatal("invalid archive left partially restored state")
			}
		})
	}
}

func TestCancelledOrEscapingSourceDoesNotCreateABackup(t *testing.T) {
	root, parent := t.TempDir(), t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	archive := filepath.Join(parent, "cancelled.age")
	_, err := Create(ctx, root, archive, testPassword, json.RawMessage(`{}`), []Selection{{Path: "missing", Name: "data"}}, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled backup: %v", err)
	}
	if _, err = os.Stat(archive); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled backup created a file")
	}
	_, err = Create(context.Background(), root, archive, testPassword, json.RawMessage(`{}`), []Selection{{Path: "../outside", Name: "data"}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatal("source escaped its owned directory")
	}
}
