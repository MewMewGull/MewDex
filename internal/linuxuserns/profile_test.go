package linuxuserns

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparationPinsThePackagedResourceAndExactAttachment(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "tools", "sandbox.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pin struct {
		Resources map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"linuxResources"`
	}
	if err := json.Unmarshal(raw, &pin); err != nil || pin.Resources["x86_64-unknown-linux-musl"].SHA256 != BubblewrapSHA256 {
		t.Fatal("preparation pin differs from the packaged helper", err)
	}
	text := profileText()
	if !strings.Contains(text, "userns,") || strings.ContainsAny(HelperPath(), "*?{}") || strings.Contains(text, "sysctl") || !strings.Contains(text, BubblewrapSHA256) {
		t.Fatal("preparation attachment is not exact", text)
	}
}
