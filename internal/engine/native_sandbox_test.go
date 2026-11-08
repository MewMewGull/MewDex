package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/config"
	"mewdex/internal/executionscope"
)

func TestNativeSandboxProcess(t *testing.T) {
	index := slices.Index(os.Args, "mewdex-native-sandbox-fixture")
	if index < 0 {
		return
	}
	mode := os.Args[index+1]
	switch os.Args[len(os.Args)-1] {
	case "--version":
		version := NativeSandboxVersion
		if mode == "legacy" {
			version = "0.1.0"
		}
		fmt.Println("mewdex-sandbox " + version)
	case "--probe":
		version := 1
		if mode == "wrong-protocol" {
			version = 2
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"protocolVersion": version, "available": mode != "unavailable"})
	default:
		var request map[string]any
		if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
			os.Exit(2)
		}
		if mode == "unconfirmed" {
			process, _ := os.FindProcess(os.Getpid())
			_ = process.Kill()
			os.Exit(3)
		}
		if mode == "command-failed" {
			os.Exit(37)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"request":                request,
			"modelCredentialPresent": os.Getenv("OPENAI_API_KEY") != "",
			"codexHomePresent":       os.Getenv("CODEX_HOME") != "",
		})
	}
	os.Exit(0)
}

func TestReadVersionDoesNotCreateAModelHome(t *testing.T) {
	settings := nativeSandboxConfig(t, "available")
	settings.Home = filepath.Join(t.TempDir(), "uncreated-home")
	version, err := ReadVersion(context.Background(), settings)
	if err != nil || version != "mewdex-sandbox "+NativeSandboxVersion {
		t.Fatal("version-only probe failed", version, err)
	}
	if _, err := os.Stat(settings.Home); !os.IsNotExist(err) {
		t.Fatal("version-only probe created model state", err)
	}
}

func nativeSandboxConfig(t *testing.T, mode string) config.Engine {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return config.Engine{Executable: program, Args: []string{"-test.run=^TestNativeSandboxProcess$", "--", "mewdex-native-sandbox-fixture", mode}, Home: t.TempDir()}
}

func TestNativeSandboxUsesBoundProfileAndExactArgv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "synthetic-native-test-key")
	t.Setenv("CODEX_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sandbox, err := NewSandbox(ctx, nativeSandboxConfig(t, "available"))
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.Version() != "mewdex-sandbox/"+NativeSandboxVersion+"/codex-"+CompatibleVersion {
		t.Fatal("native runtime reused the model runtime identity", sandbox.Version())
	}
	environment, err := sandbox.Environment()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	profile, err := executionscope.Build(executionscope.Spec{DeviceID: "native", ProjectID: "project", EngineVersion: sandbox.Version(), Workspace: root, Private: environment.Private, ReadOnly: environment.RuntimeReadOnly, AccessMode: "read-only"})
	if err != nil {
		t.Fatal(err)
	}
	argv := []string{"tool.exe", "", "目录 空格", `a"b`, `C:\trailing path\`, "&|<>", "line\nbreak"}
	command, err := sandbox.Command(ctx, profile, root, argv)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := childprocess.OutputContext(ctx, command, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Request struct {
			ProtocolVersion int             `json:"protocolVersion"`
			Workspace       string          `json:"workspace"`
			Cwd             string          `json:"cwd"`
			Command         []string        `json:"command"`
			Permissions     json.RawMessage `json:"permissions"`
		} `json:"request"`
		ModelCredentialPresent bool `json:"modelCredentialPresent"`
		CodexHomePresent       bool `json:"codexHomePresent"`
	}
	if json.Unmarshal(raw, &result) != nil || !reflect.DeepEqual(result.Request.Command, argv) || result.Request.ProtocolVersion != 1 || result.Request.Cwd != root || result.Request.Workspace != profile.Workspace() {
		t.Fatalf("native request changed its binding or argv: %s", raw)
	}
	var expected, actual any
	_ = json.Unmarshal(result.Request.Permissions, &actual)
	expectedRaw, _ := json.Marshal(profile.Definition())
	_ = json.Unmarshal(expectedRaw, &expected)
	if !reflect.DeepEqual(actual, expected) || result.ModelCredentialPresent || result.CodexHomePresent {
		t.Fatalf("native permissions or environment changed: %s", raw)
	}
	if _, err = os.Stat(filepath.Join(sandbox.runtime.home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("native sandbox created model credentials", err)
	}
}

func TestNativeSandboxCanKeepItsHostRuntimePrivate(t *testing.T) {
	program := os.Getenv("MEWDEX_TEST_SANDBOX")
	if program == "" {
		t.Skip("set MEWDEX_TEST_SANDBOX to run the native filesystem boundary check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sandbox, err := NewSandbox(ctx, config.Engine{Executable: program, Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.nativeVersion == "" {
		t.Skip("requires the independent native runtime")
	}
	environment, err := sandbox.Environment()
	if err != nil || len(environment.RuntimeReadOnly) != 0 {
		t.Fatal("host launcher was exposed to its child", environment, err)
	}
	private := append(environment.Private, filepath.Dir(program))
	root := t.TempDir()
	profile, err := executionscope.Build(executionscope.Spec{DeviceID: "native", ProjectID: "project", EngineVersion: sandbox.Version(), Workspace: root, Private: private})
	if err != nil {
		t.Fatal(err)
	}
	argv := []string{"/bin/sh", "-c", "printf PRIVATE_RUNTIME_OK"}
	if runtime.GOOS == "windows" {
		argv = []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Write-Output PRIVATE_RUNTIME_OK"}
	}
	command, err := sandbox.Command(ctx, profile, root, argv)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := childprocess.OutputContext(ctx, command, 64<<10)
	if err != nil || !strings.Contains(string(raw), "PRIVATE_RUNTIME_OK") {
		t.Fatal("native launcher required a private runtime grant", string(raw), err)
	}
}

func TestNativeSandboxKeepsLegacyRuntimeIdentity(t *testing.T) {
	sandbox, err := NewSandbox(context.Background(), nativeSandboxConfig(t, "legacy"))
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if err == nil || !strings.Contains(err.Error(), "upgrade") {
			t.Fatal("legacy POSIX sandbox ran without confirmed cleanup support", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.Version() != "mewdex-sandbox/0.1.0/codex-"+CompatibleVersion {
		t.Fatal("legacy runtime identity changed", sandbox.Version())
	}
}

func TestNativeSandboxRejectsUnavailableOrDifferentProtocol(t *testing.T) {
	for _, mode := range []string{"unavailable", "wrong-protocol"} {
		t.Run(mode, func(t *testing.T) {
			if _, err := NewSandbox(context.Background(), nativeSandboxConfig(t, mode)); err == nil {
				t.Fatal("unusable native sandbox was accepted")
			}
		})
	}
}
