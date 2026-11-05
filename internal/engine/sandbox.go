package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/config"
	"mewdex/internal/executionscope"
	"mewdex/internal/protocol"
)

// Sandbox runs the pinned engine's local sandbox command. It starts no model
// session, performs no account login and needs no connection to OpenAI.
type Sandbox struct{ runtime *Codex }

func NewSandbox(ctx context.Context, c config.Engine) (*Sandbox, error) {
	program, arguments, err := Resolve(c)
	if err != nil {
		return nil, err
	}
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(check, program, append(append([]string{}, arguments...), "--version")...)
	cmd.Env = independentEngineEnvironment(os.Environ())
	output, err := childprocess.OutputContext(check, cmd, 64<<10)
	if err != nil || strings.TrimSpace(string(output)) != "codex-cli "+CompatibleVersion {
		return nil, fmt.Errorf("local execution requires the bundled Codex %s sandbox", CompatibleVersion)
	}
	if c.Home == "" {
		return nil, errors.New("local sandbox requires a dedicated runtime home")
	}
	if err = os.MkdirAll(c.Home, 0700); err != nil {
		return nil, err
	}
	s := &Sandbox{runtime: &Codex{program: program, arguments: arguments, home: c.Home}}
	_, err = s.Environment()
	return s, err
}

func (s *Sandbox) Environment() (ExecutionEnvironment, error) {
	return s.runtime.ExecutionEnvironment()
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
	arguments = append(arguments, sandboxOverrides(profile.ThreadConfig())...)
	arguments = append(arguments, "sandbox", "--permission-profile", profile.ID(), "-C", cwd, "--")
	arguments = append(arguments, command...)
	cmd := exec.CommandContext(ctx, s.runtime.program, arguments...)
	cmd.Dir = cwd
	// The execution child receives only platform runtime variables. In
	// particular it never inherits the Hub's or an editor's model credentials.
	allowed := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TMP": true, "TEMP": true, "TMPDIR": true, "SYSTEMROOT": true, "SYSTEMDRIVE": true, "COMSPEC": true, "PATHEXT": true, "WINDIR": true, "PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "PROGRAMW6432": true, "LOCALAPPDATA": true, "APPDATA": true, "USERPROFILE": true, "HOMEDRIVE": true, "HOMEPATH": true, "LANG": true, "TZ": true, "TERM": true}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if allowed[strings.ToUpper(key)] || strings.HasPrefix(strings.ToUpper(key), "LC_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "CODEX_HOME="+filepath.Clean(s.runtime.home))
	return cmd, nil
}
