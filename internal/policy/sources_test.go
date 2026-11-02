package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigSourcesDoNotCopyCredentials(t *testing.T) {
	sources, err := ConfigSources(json.RawMessage(`{"layers":[{"name":{"type":"user","file":"/home/test/config.toml"},"config":{"api_key":"SECRET_TOKEN","developer_instructions":"exact prompt"}},{"name":{"type":"project","dotCodexFolder":"/project/.codex"},"disabledReason":"SECRET_REASON","config":{"instructions":"disabled prompt","provider":{"token":"SECRET_NESTED"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(sources)
	if strings.Contains(string(data), "SECRET") || len(sources) != 2 || sources[0].Text != "exact prompt" || sources[1].Enabled {
		t.Fatalf("incorrect source filtering: %s", data)
	}
}

func TestProjectInstructionOverrideHasPriority(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{"AGENTS.md": "ordinary", "AGENTS.override.md": "精确覆盖指令"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources := ProjectSources(root)
	if len(sources) != 1 || sources[0].Text != "精确覆盖指令" {
		t.Fatalf("wrong project source: %+v", sources)
	}
}
