// Drive actual pinned-engine tools using a deterministic loopback provider.
// No official endpoint, real account, or live installation is used.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/store"
	"mewdex/scripts/internal/responsefixture"
)

type resourceEngine struct {
	engine.Engine
	program  string
	turnMode string
}

func (e resourceEngine) Call(ctx context.Context, method string, params, out any) error {
	if method == "turn/start" && e.turnMode != "all" {
		copy := map[string]any{}
		for key, value := range params.(map[string]any) {
			copy[key] = value
		}
		delete(copy, "permissions")
		if e.turnMode == "minimal" {
			delete(copy, "cwd")
			delete(copy, "approvalPolicy")
			delete(copy, "approvalsReviewer")
		}
		params = copy
	}
	return e.Engine.Call(ctx, method, params, out)
}

func (e resourceEngine) ExecutionEnvironment() (engine.ExecutionEnvironment, error) {
	environment, err := e.Engine.ExecutionEnvironment()
	environment.RuntimeReadOnly = append(environment.RuntimeReadOnly, e.program)
	return environment, err
}

func (e resourceEngine) ReadBaseInstructions(ctx context.Context, threadID string) (string, error) {
	reader, ok := e.Engine.(engine.BaseInstructionReader)
	if !ok {
		return "", errors.New("fixture wrapper has no base-instruction reader")
	}
	return reader.ReadBaseInstructions(ctx, threadID)
}

func main() {
	if len(os.Args) == 4 && os.Args[1] == "-fixture-child" {
		childOperation(os.Args[2], os.Args[3])
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type childPaths struct {
	Target      string `json:"target"`
	Observation string `json:"observation"`
	Private     string `json:"private,omitempty"`
}

func childOperation(operation, raw string) {
	var paths childPaths
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(raw)
	if decodeErr != nil {
		decoded = []byte(raw)
	}
	if json.Unmarshal(decoded, &paths) != nil {
		os.Exit(2)
	}
	var err error
	allowed := false
	switch operation {
	case "write-project":
		err = os.WriteFile(paths.Target, []byte("mewdex-agent-tool-write"), 0600)
		allowed = err == nil
	case "read-private", "read-outside":
		_, err = os.ReadFile(paths.Target)
		allowed = err == nil
	case "network":
		var connection net.Conn
		connection, err = net.DialTimeout("tcp", paths.Target, 750*time.Millisecond)
		allowed = err == nil
		if connection != nil {
			connection.Close()
		}
	case "env":
		_, allowed = os.LookupEnv("MEWDEX_SECRET_CANARY")
	default:
		err = errors.New("unknown fixture operation")
	}
	value := map[string]any{"operation": operation, "allowed": allowed, "permissionDenied": os.IsPermission(err)}
	if paths.Private != "" {
		_, privateErr := os.ReadFile(paths.Private)
		value["privateAllowed"] = privateErr == nil
	}
	if err != nil {
		value["error"] = err.Error()
	}
	encoded := protocol.JSON(value)
	if err = os.WriteFile(paths.Observation, encoded, 0600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println(string(encoded))
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

type preparedWindows struct {
	Root        string `json:"root"`
	Workspace   string `json:"workspace"`
	Home        string `json:"home"`
	Program     string `json:"program"`
	ProgramHash string `json:"programSha256"`
	ConfigHash  string `json:"configSha256"`
	Version     string `json:"engineVersion"`
}

func loadPreparedWindows(path string) (preparedWindows, error) {
	var plan preparedWindows
	raw, err := os.ReadFile(path)
	if err != nil {
		return plan, err
	}
	if json.Unmarshal(raw, &plan) != nil || plan.Version != engine.CompatibleVersion {
		return plan, errors.New("invalid approved Windows plan")
	}
	absolute, err := filepath.Abs(path)
	if err != nil || filepath.Dir(absolute) != plan.Root || filepath.Base(absolute) != "windows-setup-plan.json" || plan.Home != filepath.Join(plan.Root, "engine-home") || plan.Workspace != filepath.Join(plan.Root, "workspace") {
		return plan, errors.New("prepared Windows layout changed")
	}
	cwd, err := os.Getwd()
	if err != nil || cwd != plan.Workspace {
		return plan, errors.New("run only from the approved prepared workspace")
	}
	for _, directory := range []string{plan.Root, plan.Home, plan.Workspace} {
		resolved, err := filepath.EvalSymlinks(directory)
		if err != nil || resolved != directory {
			return plan, errors.New("prepared Windows path was redirected")
		}
	}
	for _, check := range []struct{ path, hash string }{{plan.Program, plan.ProgramHash}, {filepath.Join(plan.Home, "config.toml"), plan.ConfigHash}} {
		data, err := os.ReadFile(check.path)
		if err != nil {
			return plan, err
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != check.hash {
			return plan, errors.New("prepared Windows engine or configuration changed")
		}
	}
	if _, err = runtimefiles.EngineResources(plan.Program, engine.CompatibleVersion); err != nil {
		return plan, err
	}
	return plan, nil
}

func fixtureCommand(helper, operation string, paths childPaths) string {
	encoded := base64.RawURLEncoding.EncodeToString(protocol.JSON(paths))
	if runtime.GOOS == "windows" {
		return "& '" + strings.ReplaceAll(helper, "'", "''") + "' -fixture-child '" + operation + "' '" + encoded + "'"
	}
	return quote(helper) + " -fixture-child " + quote(operation) + " " + quote(encoded)
}

func run() (resultErr error) {
	path := flag.String("config", ".local/device.json", "existing profile supplying only the pinned native program")
	programOverride := flag.String("engine-program", "", "explicit pinned native program; no account or source overrides are inherited")
	turnMode := flag.String("turn-mode", "all", "fixture-only override experiment: all, inherited, or minimal")
	operation := flag.String("operation", "write-project", "fixed owned-fixture operation: write-project, read-private, read-outside, network, env, patch-private")
	lifecycle := flag.Bool("lifecycle", false, "exercise a started thread after engine restart and a base-instruction fork")
	configuredEnv := flag.Bool("configured-env", false, "place the harmless environment canary in the isolated home config as well")
	windowsPlan := flag.String("windows-plan", "", "reuse exactly the approved prepared Windows home; never runs setup")
	requestPermission := flag.Bool("request-permission", false, "approve only an owned marker/network fixture, then verify turn expiry and retained private deny")
	commandApproval := flag.Bool("command-approval", false, "confirm a fixed synthetic command and verify it retains the active sandbox")
	flag.Parse()
	fileApproval := *operation == "patch-private"
	if fileApproval && (*requestPermission || *commandApproval) {
		return errors.New("patch-private uses only the synthetic file-approval flow")
	}
	if *commandApproval && *requestPermission {
		return errors.New("choose one fixture approval flow")
	}
	if *requestPermission && (!*lifecycle || (*operation != "read-outside" && *operation != "network")) {
		return errors.New("request-permission requires lifecycle and read-outside or network")
	}
	if *turnMode != "all" && *turnMode != "inherited" && *turnMode != "minimal" {
		return errors.New("unknown turn override experiment")
	}
	var prepared preparedWindows
	var program string
	var err error
	parent := ".state"
	if *windowsPlan != "" {
		prepared, err = loadPreparedWindows(*windowsPlan)
		if err != nil {
			return err
		}
		program, parent = prepared.Program, prepared.Workspace
	} else if *programOverride != "" {
		if !filepath.IsAbs(*programOverride) {
			return errors.New("engine-program must be absolute")
		}
		program, err = filepath.EvalSymlinks(*programOverride)
		if err != nil {
			return err
		}
	} else {
		if runtime.GOOS == "windows" {
			return errors.New("Windows MXC fixture requires an explicit isolated native program; legacy setup is never implicit")
		}
		source, loadErr := config.Load(*path)
		if loadErr != nil {
			return loadErr
		}
		var args []string
		program, args, err = engine.Resolve(source.Engine)
		if err != nil {
			return err
		}
		if len(args) != 0 {
			return errors.New("fixture needs the pinned native program without source overrides")
		}
	}
	root, err := os.MkdirTemp(parent, "agent-tool-risk-")
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	report := map[string]any{"root": root, "at": protocol.Now(), "platform": runtime.GOOS, "engineVersion": engine.CompatibleVersion, "responseSource": "deterministic loopback fixture", "officialInferenceRequests": 0, "realAccountUsed": false, "liveServicesUpdated": false, "systemSetupRequested": false, "finalAcceptance": false}
	report["turnOverrideExperiment"] = *turnMode
	report["operation"] = *operation
	report["configuredEnvironmentCanary"] = *configuredEnv
	report["requestPermission"] = *requestPermission
	report["commandApproval"] = *commandApproval
	defer func() {
		report["state"] = "succeeded"
		if resultErr != nil {
			report["state"], report["error"] = "failed", resultErr.Error()
		}
		if err := os.WriteFile(filepath.Join(root, "inspection.json"), protocol.JSON(report), 0600); err != nil {
			resultErr = err
		}
		fmt.Println(string(protocol.JSON(report)))
	}()
	workspace, data, home := filepath.Join(root, "workspace"), filepath.Join(root, "private-data"), filepath.Join(root, "engine-home")
	directories := []string{workspace, data, home}
	if prepared.Home != "" {
		home = prepared.Home
		directories = []string{workspace, data}
		report["preparedWindowsHome"] = home
	}
	for _, dir := range directories {
		if err = os.Mkdir(dir, 0700); err != nil {
			return err
		}
	}
	helper, err := os.Executable()
	if err != nil {
		return err
	}
	output := filepath.Join(workspace, "agent-output.txt")
	observationPath := filepath.Join(workspace, "agent-observation.json")
	fixture := responsefixture.New(root)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: fixture, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	target := output
	switch *operation {
	case "write-project":
	case "read-private":
		target = filepath.Join(data, "private-marker.txt")
	case "patch-private":
		target = filepath.Join(data, "private-patch.txt")
	case "read-outside":
		target = filepath.Join(root, "outside-marker.txt")
	case "network":
		target = listener.Addr().String()
	case "env":
		os.Setenv("MEWDEX_SECRET_CANARY", "synthetic-non-credential-marker")
	default:
		return errors.New("unknown fixed fixture operation")
	}
	if *operation == "read-private" || *operation == "read-outside" {
		if err = os.WriteFile(target, []byte("owned synthetic marker"), 0600); err != nil {
			return err
		}
	}
	privateMarker := ""
	if *requestPermission || *commandApproval || fileApproval {
		privateMarker = filepath.Join(data, "always-private-marker.txt")
		if err = os.WriteFile(privateMarker, []byte("owned private marker"), 0600); err != nil {
			return err
		}
	}
	if *operation == "network" {
		control, err := net.DialTimeout("tcp", target, time.Second)
		if err != nil {
			return err
		}
		control.Close()
		report["networkPositiveControl"] = true
	}
	text := fmt.Sprintf("model = \"gpt-6.1-sol\"\nmodel_provider = \"mewdex_fixture\"\nweb_search = \"disabled\"\n[model_providers.mewdex_fixture]\nname = \"MewDex deterministic loopback fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", "http://"+listener.Addr().String()+"/v1")
	if runtime.GOOS == "windows" {
		text += "\n[windows]\nsandbox = \"mxc\"\n"
		report["windowsSandbox"] = "mxc"
	}
	if *configuredEnv {
		text += "\n[shell_environment_policy]\ninherit = \"all\"\nset = { MEWDEX_SECRET_CANARY = \"synthetic-configured-marker\" }\n"
	}
	// The app-server otherwise persists inferred trust at thread/start (and
	// can select the enclosing checkout). Keep this fixture's saved home
	// unchanged and scope the explicit trust setting to its own workspace.
	engineArgs := []string{"--strict-config", "-c", fmt.Sprintf(`projects={%q={trust_level="trusted"}}`, workspace)}
	if prepared.Home == "" {
		if err = os.WriteFile(filepath.Join(home, "config.toml"), []byte(text), 0600); err != nil {
			return err
		}
	} else {
		if *configuredEnv {
			return errors.New("the prepared Windows config remains immutable; configured-env is Unix-only")
		}
		provider := fmt.Sprintf(`{ name = "MewDex deterministic loopback fixture", base_url = %q, wire_api = "responses", requires_openai_auth = false, supports_websockets = false, request_max_retries = 0, stream_max_retries = 0 }`, "http://"+listener.Addr().String()+"/v1")
		engineArgs = append(engineArgs, "-c", `model="gpt-6.1-sol"`, "-c", `model_provider="mewdex_fixture"`, "-c", "model_providers.mewdex_fixture="+provider)
	}
	// These environment changes affect only this short-lived probe process and
	// its own child. No credentials are read, printed, copied, or submitted.
	for _, name := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		os.Unsetenv(name)
	}
	os.Setenv("NO_PROXY", "127.0.0.1,localhost")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := config.Config{DeviceID: "agent-tool-risk-" + protocol.ID(), DataDir: data, Projects: []protocol.Project{{ID: "risk", Path: workspace}}, Engine: config.Engine{Executable: program, Args: engineArgs, Home: home}}
	var real *engine.Codex
	var state *store.Store
	var x *executor.Executor
	closeController := func() error {
		var closeErr error
		if x != nil {
			closeErr = x.Close()
			x = nil
		}
		if real != nil {
			if err := real.Close(); err != nil && closeErr == nil {
				closeErr = err
			}
			real = nil
		}
		if state != nil {
			if err := state.Close(); err != nil && closeErr == nil {
				closeErr = err
			}
			state = nil
		}
		if closeErr != nil {
			report["engineCloseError"] = closeErr.Error()
		}
		return closeErr
	}
	openController := func() error {
		var err error
		real, err = engine.Start(ctx, c.Engine)
		if err != nil {
			return err
		}
		var account struct {
			Account json.RawMessage `json:"account"`
		}
		if err = real.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
			return err
		}
		if string(account.Account) != "null" {
			return errors.New("fixture home unexpectedly has account authorization")
		}
		state, err = store.Open(filepath.Join(data, "executor.db"))
		if err != nil {
			return err
		}
		x, err = executor.New(c, state, resourceEngine{Engine: real, program: helper, turnMode: *turnMode}, nil)
		return err
	}
	defer func() {
		if state != nil {
			if events, eventErr := state.Events(0, "", 1000); eventErr == nil {
				_ = os.WriteFile(filepath.Join(root, "events-at-close.json"), protocol.JSON(events), 0600)
			}
		}
		closeErr := closeController()
		report["engineClosed"] = closeErr == nil && report["engineCloseError"] == nil
		if closeErr != nil && resultErr == nil {
			resultErr = closeErr
		}
	}()
	if err = openController(); err != nil {
		return err
	}
	created := x.Handle(ctx, protocol.Command{ID: "fixture-create", SourceID: "fixture-input", DeviceID: c.DeviceID, ProjectID: "risk", Kind: "create"})
	if created.State != "completed" || created.Session == nil {
		return fmt.Errorf("create: %s: %s", created.State, created.Error)
	}
	s := *created.Session
	report["sessionId"], report["scopeId"] = s.ID, s.ExecutionScopeID
	phases := 1
	if *lifecycle {
		phases = 3
	}
	phaseNames := []string{"created", "resumed-after-restart", "forked-base-instructions"}
	resumePhase, forkPhase := 1, 2
	if *requestPermission {
		phases, resumePhase, forkPhase = 4, 2, 3
		phaseNames = []string{"created-with-turn-grant", "next-turn-without-grant", "resumed-without-grant", "forked-without-grant"}
	}
	phaseResults := []map[string]any{}
	initialScope := s.ExecutionScopeID
	for phase := 0; phase < phases; phase++ {
		name := phaseNames[phase]
		if phase == resumePhase {
			if err = closeController(); err != nil {
				return fmt.Errorf("close before %s: %w", name, err)
			}
			if err = openController(); err != nil {
				return fmt.Errorf("open for %s: %w", name, err)
			}
			if err = state.Get("sessions", s.ID, &s); err != nil {
				return err
			}
		}
		previousEngineID := s.EngineID
		if phase == forkPhase {
			base := "MewDex local tool fixture. Preserve the configured execution scope."
			next := s.Policy
			next.BaseInstructions = &base
			changed := x.Handle(ctx, protocol.Command{ID: "fixture-base-policy", SourceID: "fixture-input", DeviceID: c.DeviceID, SessionID: s.ID, Kind: "policy.save", Policy: &next, ExpectedPolicyVersion: s.PolicyVersion})
			if changed.State != "completed" || changed.Session == nil {
				return fmt.Errorf("base policy: %s: %s", changed.State, changed.Error)
			}
			s = *changed.Session
		}
		phaseOutput := output
		if *operation == "write-project" {
			phaseOutput = filepath.Join(workspace, fmt.Sprintf("agent-output-%d.txt", phase))
			target = phaseOutput
		}
		observationPath = filepath.Join(workspace, fmt.Sprintf("agent-observation-%d.json", phase))
		command := fixtureCommand(helper, *operation, childPaths{Target: target, Observation: observationPath, Private: privateMarker})
		patch := ""
		if fileApproval {
			patch = "*** Begin Patch\n*** Add File: " + target + "\n+owned private patch fixture\n*** End Patch\n"
			command = "apply_patch <<'MEWDEX_OWNED_PATCH'\n" + patch + "MEWDEX_OWNED_PATCH"
		}
		if fileApproval {
			fixture.BeginPatch(command, patch)
		} else if *commandApproval {
			fixture.BeginCommandApproval(command)
		} else if *requestPermission && phase == 0 {
			permission := map[string]any{"file_system": map[string]any{"read": []string{target}}}
			if *operation == "network" {
				permission = map[string]any{"network": map[string]bool{"enabled": true}}
			}
			fixture.BeginPermission(command, permission)
		} else {
			fixture.Begin(command)
		}
		commandID := fmt.Sprintf("fixture-start-%d", phase)
		if err = os.WriteFile(filepath.Join(root, fmt.Sprintf("attempt-%d.json", phase)), protocol.JSON(map[string]any{"commandId": commandID, "sessionId": s.ID, "state": "submitting", "officialInferenceRequests": 0}), 0600); err != nil {
			return err
		}
		started := x.Handle(ctx, protocol.Command{ID: commandID, SourceID: "fixture-input", DeviceID: c.DeviceID, SessionID: s.ID, Kind: "start", Input: []protocol.Input{{Type: "text", Text: "Run the deterministic local fixture tool and report completion."}}})
		if started.State != "completed" || started.Session == nil {
			return fmt.Errorf("%s start: %s: %s", name, started.State, started.Error)
		}
		turnID := started.Session.TurnID
		ticker := time.NewTicker(100 * time.Millisecond)
		grantAnswered := false
		for {
			if err = state.Get("sessions", s.ID, &s); err != nil {
				ticker.Stop()
				return err
			}
			if s.State != "running" && s.State != "stopping" && s.StopStatus != "background-running" && s.StopStatus != "processes-unchecked" {
				break
			}
			requests, listErr := store.List[protocol.Interaction](state, "interactions")
			if listErr != nil {
				ticker.Stop()
				return listErr
			}
			for _, request := range requests {
				if request.SessionID != s.ID || request.State != "pending" {
					continue
				}
				if fileApproval {
					expectedPreview := "--- /dev/null\n+++ " + target + "\n+owned private patch fixture\n"
					if grantAnswered || request.Kind != "files" || !request.CanAccept || request.FilePatch != expectedPreview {
						ticker.Stop()
						return fmt.Errorf("unexpected file approval: %s: %s", request.Kind, request.Details)
					}
					answered := x.Handle(ctx, protocol.Command{ID: fmt.Sprintf("fixture-file-approval-%d", phase), SourceID: "fixture-input", DeviceID: c.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: request.ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
					if answered.State != "completed" {
						ticker.Stop()
						return errors.New(answered.Error)
					}
					grantAnswered = true
					continue
				}
				if *commandApproval {
					if grantAnswered || request.Kind != "command" || !request.CanAccept || request.Permissions != nil {
						ticker.Stop()
						return fmt.Errorf("unexpected command approval: %s: %s", request.Kind, request.Details)
					}
					answered := x.Handle(ctx, protocol.Command{ID: fmt.Sprintf("fixture-command-approval-%d", phase), SourceID: "fixture-input", DeviceID: c.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: request.ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
					if answered.State != "completed" {
						ticker.Stop()
						return errors.New(answered.Error)
					}
					grantAnswered = true
					continue
				}
				if !*requestPermission || phase != 0 || grantAnswered || request.Kind != "permissions" || !request.CanAccept || request.Permissions == nil {
					ticker.Stop()
					return fmt.Errorf("unexpected fixture interaction: %s: %s", request.Kind, request.Details)
				}
				grant := request.Permissions
				if len(grant.WritePaths) != 0 || (*operation == "network" && (!grant.Network || len(grant.ReadPaths) != 0)) || (*operation == "read-outside" && (grant.Network || len(grant.ReadPaths) != 1 || grant.ReadPaths[0] != target)) {
					ticker.Stop()
					return errors.New("fixture permission exceeds its synthetic target")
				}
				answered := x.Handle(ctx, protocol.Command{ID: "fixture-owned-grant", SourceID: "fixture-input", DeviceID: c.DeviceID, SessionID: s.ID, Kind: "respond", RequestID: request.ID, Response: protocol.JSON(protocol.InteractionReply{Action: "accept"})})
				if answered.State != "completed" {
					ticker.Stop()
					return errors.New(answered.Error)
				}
				grantAnswered = true
			}
			select {
			case <-ctx.Done():
				ticker.Stop()
				return ctx.Err()
			case <-ticker.C:
			}
		}
		ticker.Stop()
		if s.State != "idle" || s.Frozen || s.ExecutionScopeID != initialScope {
			return errors.New("fixture turn did not settle with its original bound scope")
		}
		if phase > 0 && phase < forkPhase && s.EngineID != previousEngineID {
			return errors.New("a started conversation was replaced instead of resumed")
		}
		if phase == forkPhase && s.EngineID == previousEngineID {
			return errors.New("base instructions did not create a history-preserving fork")
		}
		if fileApproval {
			_, patchErr := os.Stat(target)
			marker, markerErr := os.ReadFile(privateMarker)
			preserved := markerErr == nil && string(marker) == "owned private marker"
			phaseResult := map[string]any{"phase": name, "turnId": turnID, "engineId": s.EngineID, "scopeId": s.ExecutionScopeID, "grantAnswered": grantAnswered, "privatePatchAbsent": os.IsNotExist(patchErr), "privateMarkerPreserved": preserved}
			phaseResults = append(phaseResults, phaseResult)
			report["phases"] = phaseResults
			if !grantAnswered || !os.IsNotExist(patchErr) || !preserved {
				return errors.New("file approval did not preserve its private boundary")
			}
			continue
		}
		result, err := os.ReadFile(observationPath)
		if err != nil {
			return fmt.Errorf("native agent tool did not return its observation: %v", err)
		}
		var observation struct {
			Operation      string `json:"operation"`
			Allowed        bool   `json:"allowed"`
			PrivateAllowed bool   `json:"privateAllowed"`
		}
		if json.Unmarshal(result, &observation) != nil || observation.Operation != *operation {
			return errors.New("invalid native agent observation")
		}
		phaseResult := map[string]any{"phase": name, "turnId": turnID, "engineId": s.EngineID, "scopeId": s.ExecutionScopeID, "toolObservation": json.RawMessage(result), "grantAnswered": grantAnswered}
		phaseResults = append(phaseResults, phaseResult)
		report["phases"] = phaseResults
		if observation.Allowed != (*operation == "write-project" || *requestPermission && phase == 0) || observation.PrivateAllowed || (*requestPermission && phase == 0 || *commandApproval) && !grantAnswered {
			return errors.New("native agent tool crossed its expected filesystem, network or environment boundary")
		}
		if *operation == "write-project" {
			result, err = os.ReadFile(phaseOutput)
			if err != nil || string(result) != "mewdex-agent-tool-write" {
				return errors.New("native agent project write did not persist")
			}
		}
	}
	if *operation == "read-private" || *operation == "read-outside" {
		content, readErr := os.ReadFile(target)
		if readErr != nil || string(content) != "owned synthetic marker" {
			return errors.New("owned host marker changed during the fixture")
		}
		report["hostReadMarkerPreserved"] = true
	}
	report["sessionState"] = s.State
	fixtureState := fixture.Snapshot()
	report["fixtureRequests"], report["shellTool"], report["namespace"], report["fixtureError"] = fixtureState.Requests, fixtureState.Tool, fixtureState.Namespace, fixtureState.Error
	events, err := state.Events(0, s.ID, 1000)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "events.json"), protocol.JSON(events), 0600); err != nil {
		return err
	}
	report["nativeAgentToolOutputVerified"] = true
	return nil
}
