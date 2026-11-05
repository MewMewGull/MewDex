package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/config"
	"mewdex/internal/executionscope"
)

func TestStandaloneSandboxNeedsNoModelAccount(t *testing.T) {
	program := os.Getenv("MEWDEX_TEST_SANDBOX")
	if program == "" {
		t.Skip("set MEWDEX_TEST_SANDBOX to the pinned native runtime for the local sandbox check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root, home := t.TempDir(), t.TempDir()
	sandbox, err := NewSandbox(ctx, config.Engine{Executable: program, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	environment, err := sandbox.Environment()
	if err != nil {
		t.Fatal(err)
	}
	profile, err := executionscope.Build(executionscope.Spec{DeviceID: "model-free-node", ProjectID: "project", EngineVersion: CompatibleVersion, Workspace: root, Private: environment.Private, ReadOnly: environment.RuntimeReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	argv := []string{"/bin/sh", "-c", "printf hub-execution-ok > result.txt; cat result.txt"}
	if runtime.GOOS == "windows" {
		argv = []string{"powershell.exe", "-NoProfile", "-Command", "Set-Content -LiteralPath result.txt -Value hub-execution-ok; Get-Content -LiteralPath result.txt"}
	}
	command, err := sandbox.Command(ctx, profile, root, argv)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := childprocess.OutputContext(ctx, command, 64<<10)
	if err != nil || !strings.Contains(string(output), "hub-execution-ok") {
		t.Fatalf("sandbox execution failed %s %s %v", output, diagnostics.String(), err)
	}
	if _, err = os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("model-free execution created account credentials", err)
	}
}
