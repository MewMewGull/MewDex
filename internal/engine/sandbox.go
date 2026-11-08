package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/config"
	"mewdex/internal/executionscope"
	"mewdex/internal/linuxuserns"
	"mewdex/internal/protocol"
)

const NativeSandboxVersion = "0.1.2"

// Sandbox owns a model-free local runtime. Existing installations can also use
// the pinned engine's sandbox command while their runtime upgrade is prepared.
type Sandbox struct {
	runtime       *Codex
	nativeVersion string
}

func NewSandbox(ctx context.Context, c config.Engine) (*Sandbox, error) {
	program, arguments, err := Resolve(c)
	if err != nil {
		return nil, err
	}
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	version, err := executableVersion(check, program, arguments)
	nativeVersion := strings.TrimPrefix(version, "mewdex-sandbox ")
	native := nativeVersion != version && (nativeVersion == NativeSandboxVersion || nativeVersion == "0.1.1" || nativeVersion == "0.1.0")
	if err != nil || (!native && version != "codex-cli "+CompatibleVersion) {
		return nil, fmt.Errorf("local execution requires MewDex sandbox %s or Codex %s", NativeSandboxVersion, CompatibleVersion)
	}
	if native && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") && nativeVersion != NativeSandboxVersion {
		return nil, errors.New("upgrade the native POSIX sandbox before executing new work")
	}
	if native {
		probe := exec.CommandContext(check, program, append(append([]string{}, arguments...), "--probe")...)
		probe.Env = independentEngineEnvironment(os.Environ())
		probe.Env, err = linuxuserns.Environment(probe.Env)
		if err != nil {
			return nil, err
		}
		raw, probeErr := childprocess.OutputContext(check, probe, 64<<10)
		var result struct {
			ProtocolVersion int  `json:"protocolVersion"`
			Available       bool `json:"available"`
		}
		if probeErr != nil || json.Unmarshal(raw, &result) != nil || result.ProtocolVersion != 1 || !result.Available {
			message := "native local sandbox is unavailable on this system"
			if guidance := linuxuserns.Guidance(); guidance != "" {
				message += " " + guidance
			}
			return nil, errors.New(message)
		}
	}
	if c.Home == "" {
		return nil, errors.New("local sandbox requires a dedicated runtime home")
	}
	if err = os.MkdirAll(c.Home, 0700); err != nil {
		return nil, err
	}
	s := &Sandbox{runtime: &Codex{program: program, arguments: arguments, home: c.Home}}
	if native {
		s.nativeVersion = nativeVersion
	}
	_, err = s.Environment()
	return s, err
}

func (s *Sandbox) Version() string {
	if s.nativeVersion != "" {
		return "mewdex-sandbox/" + s.nativeVersion + "/codex-" + CompatibleVersion
	}
	return CompatibleVersion
}

func (s *Sandbox) Environment() (ExecutionEnvironment, error) {
	environment, err := s.runtime.ExecutionEnvironment()
	if s.nativeVersion != "" {
		// The host launcher creates the sandbox around the requested tool.
		// Its own executable stays outside the child's filesystem grants.
		environment.RuntimeReadOnly = nil
	}
	return environment, err
}

// Each top-level value is passed as one TOML value. CLI dotted-key overrides
// split at periods, so native paths must stay inside quoted inline-table keys.
func sandboxOverrides(settings map[string]any) []string {
	var normalized map[string]any
	_ = json.Unmarshal(protocol.JSON(settings), &normalized)
	keys := make([]string, 0, len(normalized))
	for key := range normalized {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	arguments := []string{}
	for _, key := range keys {
		arguments = append(arguments, "-c", key+"="+sandboxTOML(normalized[key]))
	}
	return arguments
}

func sandboxTOML(value any) string {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries := make([]string, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, string(protocol.JSON(key))+"="+sandboxTOML(value[key]))
		}
		return "{" + strings.Join(entries, ",") + "}"
	case []any:
		entries := make([]string, 0, len(value))
		for _, entry := range value {
			entries = append(entries, sandboxTOML(entry))
		}
		return "[" + strings.Join(entries, ",") + "]"
	default:
		return string(protocol.JSON(value))
	}
}

func (s *Sandbox) Command(ctx context.Context, profile executionscope.Profile, cwd string, command []string) (*exec.Cmd, error) {
	if len(command) == 0 || command[0] == "" {
		return nil, errors.New("a command argv is required")
	}
	arguments := append([]string{}, s.runtime.arguments...)
	if s.nativeVersion == "" {
		arguments = append(arguments, sandboxOverrides(profile.ThreadConfig())...)
		arguments = append(arguments, "sandbox", "--permission-profile", profile.ID(), "-C", cwd, "--")
		arguments = append(arguments, command...)
	}
	cmd := exec.CommandContext(ctx, s.runtime.program, arguments...)
	cmd.Dir = cwd
	if s.nativeVersion != "" {
		request := map[string]any{"protocolVersion": 1, "workspace": profile.Workspace(), "cwd": cwd, "command": command, "permissions": profile.Definition()}
		raw := protocol.JSON(request)
		if len(raw) > 1<<20 {
			return nil, errors.New("sandbox request is too large")
		}
		cmd.Stdin = strings.NewReader(string(raw))
		configureNativeSandboxStop(cmd)
	}
	// The execution child receives only platform runtime variables. In
	// particular it never inherits the Hub's or an editor's model credentials.
	allowed := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TMP": true, "TEMP": true, "TMPDIR": true, "SYSTEMROOT": true, "SYSTEMDRIVE": true, "COMSPEC": true, "PATHEXT": true, "WINDIR": true, "PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "PROGRAMW6432": true, "LOCALAPPDATA": true, "APPDATA": true, "USERPROFILE": true, "HOMEDRIVE": true, "HOMEPATH": true, "LANG": true, "TZ": true, "TERM": true}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if allowed[strings.ToUpper(key)] || strings.HasPrefix(strings.ToUpper(key), "LC_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if s.nativeVersion == "" {
		cmd.Env = append(cmd.Env, "CODEX_HOME="+filepath.Clean(s.runtime.home))
	} else {
		var err error
		cmd.Env, err = linuxuserns.Environment(cmd.Env)
		if err != nil {
			return nil, err
		}
	}
	return cmd, nil
}
