package engine

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mewdex/internal/runtimefiles"
)

// ExecutionEnvironment describes the actual engine process, not a second
// independent resolution of the user's config by the executor.
type ExecutionEnvironment struct {
	RuntimeReadOnly []string
	Private         []string
}

func (e *Codex) ExecutionEnvironment() (ExecutionEnvironment, error) {
	var result ExecutionEnvironment
	if e.program == "" || e.home == "" {
		return result, errors.New("engine runtime and account home must be known before execution")
	}
	for _, arg := range e.arguments {
		if strings.HasSuffix(strings.ToLower(arg), ".js") {
			return result, errors.New("execution scopes require the pinned native engine program; configure the bundled engine instead of a JavaScript launcher")
		}
	}
	program, err := exec.LookPath(e.program)
	if err != nil {
		return result, err
	}
	program, err = filepath.Abs(program)
	if err != nil {
		return result, err
	}
	manifestPath := filepath.Join(filepath.Dir(filepath.Dir(program)), "mewdex-engine.json")
	if _, err = os.Stat(manifestPath); err == nil {
		result.RuntimeReadOnly, err = runtimefiles.EngineResources(program, CompatibleVersion)
		if err != nil {
			return result, err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// A standalone, version-checked native engine gets only its executable.
		// Missing companion grants cause a sandbox error; no directory fallback.
		result.RuntimeReadOnly = []string{program}
	} else {
		return result, err
	}
	result.Private = []string{e.home}
	return result, nil
}
