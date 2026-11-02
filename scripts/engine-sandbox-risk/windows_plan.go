package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
)

type windowsPlan struct {
	Format            int    `json:"format"`
	Root              string `json:"root"`
	Home              string `json:"home"`
	Workspace         string `json:"workspace"`
	Program           string `json:"program"`
	ProgramSHA256     string `json:"programSha256"`
	ConfigSHA256      string `json:"configSha256"`
	EngineManifestSHA string `json:"engineManifestSha256"`
	EngineVersion     string `json:"engineVersion"`
	HelperSHA256      string `json:"helperSha256"`
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return hashReader(file)
}

func hashReader(reader io.Reader) (string, error) {
	digest := sha256.New()
	if _, err := io.Copy(digest, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func planWindows(source string) error {
	if engine.CompatibleVersion != "0.154.0" {
		return errors.New("legacy setup plans are retired; the current MXC implementation needs no machine-wide setup")
	}
	if runtime.GOOS != "windows" {
		return errors.New("this candidate is for the existing Windows host")
	}
	root, err := os.MkdirTemp(".state", "engine-sandbox-setup-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	program, err := runtimefiles.Engine(root, source, engine.CompatibleVersion)
	if err != nil {
		return err
	}
	plan := windowsPlan{Format: 1, Root: root, Home: filepath.Join(root, "engine-home"), Workspace: filepath.Join(root, "workspace"), Program: program, EngineVersion: engine.CompatibleVersion}
	for _, path := range []string{plan.Home, plan.Workspace} {
		if err = os.Mkdir(path, 0700); err != nil {
			return err
		}
	}
	profile := fmt.Sprintf("default_permissions = \"mewdex-probe\"\n[windows]\nsandbox = \"elevated\"\n[permissions.mewdex-probe.filesystem]\n\":minimal\" = \"read\"\n%q = \"write\"\n%q = \"read\"\n[permissions.mewdex-probe.network]\nenabled = false\n", plan.Workspace, filepath.Dir(filepath.Dir(program)))
	if err = os.WriteFile(filepath.Join(plan.Home, "config.toml"), []byte(profile), 0600); err != nil {
		return err
	}
	plan.ProgramSHA256, err = hashFile(program)
	if err != nil {
		return err
	}
	plan.ConfigSHA256, err = hashFile(filepath.Join(plan.Home, "config.toml"))
	if err != nil {
		return err
	}
	plan.EngineManifestSHA, err = hashFile(filepath.Join(filepath.Dir(filepath.Dir(program)), "mewdex-engine.json"))
	if err != nil {
		return err
	}
	helper, err := os.Executable()
	if err != nil {
		return err
	}
	plan.HelperSHA256, err = hashFile(helper)
	if err != nil {
		return err
	}
	path := filepath.Join(root, "windows-setup-plan.json")
	if err = os.WriteFile(path, protocol.JSON(plan), 0600); err != nil {
		return err
	}
	fmt.Println(string(protocol.JSON(map[string]any{"prepared": true, "path": path, "root": root, "engineVersion": plan.EngineVersion, "programSha256": plan.ProgramSHA256, "engineManifestSha256": plan.EngineManifestSHA, "helperSha256": plan.HelperSHA256, "setupRequested": false, "modelTurnsSubmitted": 0})))
	return nil
}

// validateWindowsPlan performs no setup and makes no runtime copies. Every
// companion executable must still match the reviewed, retained manifest.
func validateWindowsPlan(path, helper string, requireWorkspace bool) (windowsPlan, error) {
	var plan windowsPlan
	path, err := filepath.Abs(path)
	if err != nil {
		return plan, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return plan, err
	}
	if len(raw) > 64<<10 || json.Unmarshal(raw, &plan) != nil || plan.Format != 1 || plan.EngineVersion != engine.CompatibleVersion {
		return plan, errors.New("invalid retained Windows setup plan")
	}
	root, err := filepath.EvalSymlinks(plan.Root)
	if err != nil || root != plan.Root || filepath.Dir(path) != root || filepath.Base(path) != "windows-setup-plan.json" || plan.Home != filepath.Join(root, "engine-home") || plan.Workspace != filepath.Join(root, "workspace") {
		return plan, errors.New("Windows setup plan directory changed")
	}
	for _, directory := range []string{plan.Home, plan.Workspace} {
		resolved, err := filepath.EvalSymlinks(directory)
		if err != nil || resolved != directory {
			return plan, errors.New("Windows setup home or workspace was redirected")
		}
	}
	if requireWorkspace {
		cwd, err := os.Getwd()
		if err != nil || filepath.Clean(cwd) != plan.Workspace {
			return plan, errors.New("run only from the retained candidate workspace, so setup does not target the source checkout")
		}
	}
	manifestDigest, err := hex.DecodeString(plan.EngineManifestSHA)
	if err != nil || len(manifestDigest) != sha256.Size {
		return plan, errors.New("invalid retained runtime manifest digest")
	}
	runtimeRoot := filepath.Join(root, "engines", plan.EngineVersion+"-"+plan.EngineManifestSHA[:32])
	if plan.Program != filepath.Join(runtimeRoot, "bin", "codex.exe") {
		return plan, errors.New("Windows setup program escaped its retained runtime")
	}
	for _, check := range []struct{ path, expected string }{{plan.Program, plan.ProgramSHA256}, {filepath.Join(plan.Home, "config.toml"), plan.ConfigSHA256}, {filepath.Join(runtimeRoot, "mewdex-engine.json"), plan.EngineManifestSHA}, {helper, plan.HelperSHA256}} {
		resolved, resolveErr := filepath.EvalSymlinks(check.path)
		if resolveErr != nil || resolved != check.path {
			return plan, errors.New("Windows candidate file was redirected")
		}
		actual, err := hashFile(check.path)
		if err != nil || actual != check.expected {
			return plan, errors.New("Windows candidate program or configuration changed; preserve the original plan")
		}
	}
	input, err := os.OpenRoot(runtimeRoot)
	if err != nil {
		return plan, err
	}
	defer input.Close()
	manifestFile, err := input.Open("mewdex-engine.json")
	if err != nil {
		return plan, err
	}
	defer manifestFile.Close()
	var manifest runtimefiles.Manifest
	if json.NewDecoder(io.LimitReader(manifestFile, 256<<10+1)).Decode(&manifest) != nil || manifest.Format != 1 || manifest.Version != plan.EngineVersion || len(manifest.Files) == 0 {
		return plan, errors.New("invalid retained runtime manifest")
	}
	seen := map[string]bool{}
	for _, spec := range manifest.Files {
		relative := filepath.FromSlash(spec.Path)
		if !filepath.IsLocal(relative) || relative == "." || strings.Contains(spec.Path, "\\") || seen[relative] {
			return plan, errors.New("invalid retained runtime companion path")
		}
		seen[relative] = true
		file, err := input.Open(relative)
		if err != nil {
			return plan, err
		}
		actual, hashErr := hashReader(file)
		info, statErr := file.Stat()
		file.Close()
		if hashErr != nil || statErr != nil || !info.Mode().IsRegular() || info.Size() != spec.Bytes || actual != spec.SHA256 {
			return plan, errors.New("retained Windows runtime companion changed")
		}
	}
	if !seen[filepath.Join("bin", "codex.exe")] {
		return plan, errors.New("Windows entry point is absent from retained manifest")
	}
	return plan, nil
}

func startWindowsCandidate(ctx context.Context, plan windowsPlan) (*engine.Codex, error) {
	e, err := engine.Start(ctx, config.Engine{Executable: plan.Program, Home: plan.Home, Args: []string{"--strict-config"}})
	if err != nil {
		return nil, err
	}
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if err = e.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err == nil && string(account.Account) != "null" {
		err = errors.New("sandbox candidate unexpectedly has model authorization")
	}
	if err != nil {
		e.Close()
		return nil, err
	}
	return e, nil
}

func observeWindows(path string) error {
	if runtime.GOOS != "windows" {
		return errors.New("Windows only")
	}
	helper, err := os.Executable()
	if err != nil {
		return err
	}
	plan, err := validateWindowsPlan(path, helper, false)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	e, err := startWindowsCandidate(ctx, plan)
	if err != nil {
		return err
	}
	defer e.Close()
	go func() {
		for notification := range e.Notifications() {
			if len(notification.RequestID) > 0 {
				_ = e.Reject(ctx, notification.RequestID, "Read-only sandbox readiness inspection")
			}
		}
	}()
	var readiness any
	if err = e.Call(ctx, "windowsSandbox/readiness", nil, &readiness); err != nil {
		return err
	}
	value := protocol.JSON(map[string]any{"at": protocol.Now(), "readiness": readiness, "unsignedHome": true, "setupRequested": false, "modelTurnsSubmitted": 0})
	if err = os.WriteFile(filepath.Join(plan.Root, "setup-observation.json"), value, 0600); err != nil {
		return err
	}
	fmt.Println(string(value))
	return nil
}

func applyWindows(path string) error {
	if runtime.GOOS != "windows" {
		return errors.New("Windows only")
	}
	helper, err := os.Executable()
	if err != nil {
		return err
	}
	plan, err := validateWindowsPlan(path, helper, true)
	if err != nil {
		return err
	}
	// Check before launching even an unsigned engine. O_EXCL below remains the
	// authority if two explicit invocations race; neither ever repeats setup.
	if _, err = os.Stat(filepath.Join(plan.Root, "setup-attempt.json")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("a setup attempt may already exist; use windows-observe, never repeat an unknown setup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	e, err := startWindowsCandidate(ctx, plan)
	if err != nil {
		return err
	}
	defer e.Close()
	completed := make(chan json.RawMessage, 1)
	go func() {
		for notification := range e.Notifications() {
			if notification.Method == "windowsSandbox/setupCompleted" {
				select {
				case completed <- notification.Params:
				default:
				}
			}
			if len(notification.RequestID) > 0 {
				_ = e.Reject(ctx, notification.RequestID, "No model or tool request is authorized by sandbox setup")
			}
		}
	}()
	var before any
	if err = e.Call(ctx, "windowsSandbox/readiness", nil, &before); err != nil {
		return err
	}
	attempt := filepath.Join(plan.Root, "setup-attempt.json")
	file, err := os.OpenFile(attempt, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("a setup attempt already exists; inspect readiness and the original result without repeating setup")
	}
	_, err = file.Write(protocol.JSON(map[string]any{"at": protocol.Now(), "state": "requesting", "mode": "elevated", "readinessBefore": before, "modelTurnsSubmitted": 0}))
	if err == nil {
		err = file.Sync()
	}
	file.Close()
	if err != nil {
		return err
	}
	var started struct {
		Started bool `json:"started"`
	}
	if err = e.Call(ctx, "windowsSandbox/setupStart", map[string]string{"mode": "elevated"}, &started); err != nil {
		_ = os.WriteFile(filepath.Join(plan.Root, "setup-result.json"), protocol.JSON(map[string]any{"state": "unconfirmed", "error": err.Error(), "automaticRetry": false}), 0600)
		return err
	}
	if !started.Started {
		err = errors.New("the pinned engine did not confirm starting sandbox preparation")
		_ = os.WriteFile(filepath.Join(plan.Root, "setup-result.json"), protocol.JSON(map[string]any{"state": "unconfirmed", "error": err.Error(), "automaticRetry": false}), 0600)
		return err
	}
	select {
	case result := <-completed:
		if err = os.WriteFile(filepath.Join(plan.Root, "setup-result.json"), result, 0600); err != nil {
			return err
		}
		return checkSetupCompletion(result)
	case <-ctx.Done():
		_ = os.WriteFile(filepath.Join(plan.Root, "setup-result.json"), protocol.JSON(map[string]any{"state": "unconfirmed", "error": ctx.Err().Error(), "automaticRetry": false}), 0600)
		return ctx.Err()
	}
}

func checkSetupCompletion(raw json.RawMessage) error {
	var result struct {
		Mode    string `json:"mode"`
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Mode != "elevated" || result.Success == nil {
		return errors.New("invalid sandbox setup completion; preserve and inspect without retrying")
	}
	if !*result.Success {
		return fmt.Errorf("sandbox preparation failed: %s", result.Error)
	}
	return nil
}
