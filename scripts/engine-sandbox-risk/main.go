// Inspect the pinned engine's permissions in an unsigned, project-owned home.
// Fixed command probes are opt-in on Unix. No model turn or OS sandbox setup.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"mewdex/internal/attachments"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executionscope"
	"mewdex/internal/protocol"
)

func main() {
	if len(os.Args) == 3 && (os.Args[1] == "windows-plan" || os.Args[1] == "windows-apply" || os.Args[1] == "windows-observe" || os.Args[1] == "windows-commands") {
		var err error
		if os.Args[1] == "windows-plan" {
			err = planWindows(os.Args[2])
		} else if os.Args[1] == "windows-apply" {
			err = applyWindows(os.Args[2])
		} else if os.Args[1] == "windows-commands" {
			err = probePreparedWindows(os.Args[2])
		} else {
			err = observeWindows(os.Args[2])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "-sandbox-child" {
		child(os.Args[2], os.Args[3])
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (resultErr error) {
	existing := flag.Bool("inspect-existing", false, "read only the existing engine configuration and Windows sandbox readiness")
	profileThread := flag.Bool("profile-thread", false, "create an unsigned thread with the isolated permission profile; no turn or command")
	inlineProfile := flag.Bool("inline-profile", false, "define a distinct thread-only profile through thread/start.config")
	privateBoundary := flag.Bool("private-boundary", false, "protect private data nested inside the project, with read-only inputs kept outside private data")
	stagedInput := flag.Bool("staged-input", false, "use the shared integrity-checked session attachment view for the fixed Unix command probes")
	ancestorMove := flag.Bool("ancestor-move", false, "check whether moving a private directory's writable ancestor exposes its files")
	configPath := flag.String("config", ".local/device.json", "existing development profile supplying only the pinned engine program")
	commands := flag.Bool("commands", false, "run fixed unsigned filesystem/network probes on Mac or Linux only")
	flag.Parse()
	if *existing && *profileThread {
		return errors.New("profile-thread requires the isolated unsigned home")
	}
	if *commands && (*existing || runtime.GOOS == "windows") {
		return errors.New("command probes require an isolated Unix home; Windows system setup is never implicit")
	}
	if *inlineProfile && (!*profileThread || *existing || *commands) {
		return errors.New("inline-profile requires an isolated thread-only probe")
	}
	if *privateBoundary && *existing {
		return errors.New("private-boundary only uses an isolated unsigned home")
	}
	if *stagedInput && (!*commands || !*privateBoundary) {
		return errors.New("staged-input requires isolated private-boundary command probes")
	}
	if *ancestorMove && (!*commands || !*privateBoundary) {
		return errors.New("ancestor-move requires isolated private-boundary commands")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp(".state", "engine-sandbox-risk-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	report := map[string]any{"at": protocol.Now(), "platform": runtime.GOOS, "root": root, "engineVersion": engine.CompatibleVersion,
		"unsignedHome": !*existing, "existingConfigInspected": *existing, "modelTurnsSubmitted": 0, "commandsExecuted": 0,
		"sandboxSetupRequested": false, "finalAcceptance": false}
	defer func() {
		report["state"] = "succeeded"
		if resultErr != nil {
			report["state"], report["error"] = "failed", resultErr.Error()
		}
		if saveErr := os.WriteFile(filepath.Join(root, "inspection.json"), protocol.JSON(report), 0600); saveErr != nil {
			report["state"], report["evidenceError"] = "failed", saveErr.Error()
			if resultErr == nil {
				resultErr = saveErr
			}
		}
		fmt.Println(string(protocol.JSON(report)))
	}()
	workspace := filepath.Join(root, "workspace")
	home := filepath.Join(root, "engine-home")
	inputDirectory := filepath.Join(root, "readonly-input")
	private := ""
	if *privateBoundary {
		private = filepath.Join(workspace, ".mewdex-private")
		if *ancestorMove {
			private = filepath.Join(workspace, "owned-container", ".mewdex-private")
		}
	}
	directories := []string{workspace, home}
	if !*stagedInput {
		directories = append(directories, inputDirectory)
	}
	if private != "" {
		directories = append(directories, private)
	}
	for _, directory := range directories {
		if err = os.MkdirAll(directory, 0700); err != nil {
			return err
		}
	}
	inputPath, originalInputPath := "", ""
	if *stagedInput {
		assets, err := attachments.New(filepath.Join(private, "attachments"))
		if err != nil {
			return err
		}
		meta, err := assets.SaveTarget("input.txt", strings.NewReader("mewdex-preserve-marker"), "probe-input", "mewdex-risk", "sandbox-profile")
		if err != nil {
			return err
		}
		view, err := attachments.NewView(inputDirectory, attachments.ViewTarget{DeviceID: "mewdex-risk", ProjectID: "sandbox-profile", SessionID: "probe-session"})
		if err != nil {
			return err
		}
		inputPath, err = view.Stage(assets, meta.ID)
		if err != nil {
			return err
		}
		originalInputPath, err = assets.Path(meta.ID)
		if err != nil {
			return err
		}
		report["stagedAttachment"] = map[string]any{"id": meta.ID, "sha256": meta.SHA256, "bytes": meta.Size, "path": inputPath, "originalPrivatePath": originalInputPath, "integrityChecked": true}
	}
	engineConfig := cfg.Engine
	var profile executionscope.Profile
	profileName := ""
	if !*existing {
		program, programErr := os.Executable()
		if programErr != nil {
			return programErr
		}
		engineProgram, _, resolveErr := engine.Resolve(cfg.Engine)
		if resolveErr != nil {
			return resolveErr
		}
		spec := executionscope.Spec{DeviceID: "mewdex-risk", ProjectID: "sandbox-profile", EngineVersion: engine.CompatibleVersion, Workspace: workspace, ReadOnly: []string{inputDirectory, program, engineProgram}}
		if private != "" {
			spec.Private = []string{private}
		}
		profile, err = executionscope.Build(spec)
		if err != nil {
			return err
		}
		profileName = profile.ID()
		if *inlineProfile {
			profileName = "mewdex-base-only"
		}
		var text strings.Builder
		fmt.Fprintf(&text, "default_permissions = %q\n[permissions.%s.filesystem]\n", profileName, profileName)
		filesystem := profile.Definition()["filesystem"].(map[string]string)
		paths := make([]string, 0, len(filesystem))
		for path := range filesystem {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			fmt.Fprintf(&text, "%q = %q\n", path, filesystem[path])
		}
		fmt.Fprintf(&text, "[permissions.%s.network]\nenabled = false\n", profileName)
		if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(text.String()), 0600); err != nil {
			return err
		}
		engineConfig = config.Engine{Executable: cfg.Engine.Executable, Args: append(cfg.Engine.Args, "--strict-config"), Home: home}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	e, err := engine.Start(ctx, engineConfig)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := e.Close()
		report["engineClosed"] = closeErr == nil
		if closeErr != nil && resultErr == nil {
			resultErr = closeErr
		}
	}()
	go func() {
		for notification := range e.Notifications() {
			if len(notification.RequestID) > 0 {
				_ = e.Reject(ctx, notification.RequestID, "This read-only probe does not authorize engine requests")
			}
		}
	}()
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if !*existing {
		if err = e.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
			return err
		}
		if len(account.Account) > 0 && string(account.Account) != "null" {
			return errors.New("the isolated home unexpectedly has model authorization")
		}
	}
	var response struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err = e.Call(ctx, "config/read", map[string]any{"cwd": workspace, "includeLayers": false}, &response); err != nil {
		return err
	}
	selected := map[string]json.RawMessage{}
	for _, name := range []string{"default_permissions", "permissions", "sandbox_mode", "sandbox_workspace_write", "windows"} {
		if value, ok := response.Config[name]; ok {
			selected[name] = value
		}
	}
	report["config"] = selected
	var readiness any
	if runtime.GOOS == "windows" {
		if err = e.Call(ctx, "windowsSandbox/readiness", nil, &readiness); err != nil {
			return err
		}
	}
	report["windowsSandboxReadiness"] = readiness
	var threadConfig map[string]json.RawMessage
	if *profileThread {
		var result map[string]json.RawMessage
		profileName = profile.ID()
		params := map[string]any{"cwd": workspace, "permissions": profileName, "approvalPolicy": "on-request", "approvalsReviewer": "user"}
		if *inlineProfile {
			params["config"] = profile.ThreadConfig()
			report["threadInlineConfig"] = params["config"]
		}
		report["expectedPermissionProfile"] = profileName
		err = e.Call(ctx, "thread/start", params, &result)
		if err != nil {
			return err
		}
		threadConfig = map[string]json.RawMessage{}
		for key, value := range result {
			if key != "thread" {
				threadConfig[key] = value
			}
		}
		report["threadConfig"] = threadConfig
		if err = profile.VerifyThread(protocol.JSON(result)); err != nil {
			return err
		}
	}
	report["threadConfig"] = threadConfig
	if *commands {
		return runCommands(ctx, e, commandProbePaths{Root: root, Workspace: workspace, Inputs: inputDirectory, Private: private, ProfileID: profile.ID(), StagedInput: inputPath, SourceAttachment: originalInputPath, MoveAncestor: *ancestorMove}, report)
	}
	return nil
}
