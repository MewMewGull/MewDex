//go:build darwin || linux

// managed-exit-risk exercises the production startup backend and managed entry
// with bounded, model-free process fixtures. It never changes a shared service.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mewdex/internal/childprocess"
	"mewdex/internal/config"
	"mewdex/internal/lifecycle"
	"mewdex/internal/localnode"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
	"mewdex/internal/serviceguard"
	"mewdex/internal/startup"
	"mewdex/scripts/internal/responsefixture"
)

type participant struct {
	PID   int    `json:"pid"`
	Group int    `json:"group"`
	Root  string `json:"root"`
	Role  string `json:"role"`
}

type generation struct {
	PID       int              `json:"pid"`
	Group     int              `json:"group"`
	Root      string           `json:"root"`
	Execution *nativeExecution `json:"execution,omitempty"`
}

func save(file string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, raw, 0600)
}

func main() {
	var err error
	if len(os.Args) > 1 && (os.Args[1] == "process-owner" || os.Args[1] == "process-child") {
		err = serviceguard.Dispatch(os.Args[1:])
	} else if len(os.Args) > 1 && os.Args[1] == "managed" {
		err = managed(os.Args[2:])
	} else if len(os.Args) > 1 && os.Args[1] == "fixture" {
		err = fixture(os.Args[2:])
	} else {
		err = probe()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fixture(arguments []string) error {
	if len(arguments) != 2 || !filepath.IsAbs(arguments[0]) {
		return errors.New("fixture requires an absolute owned generation and role")
	}
	root, role := arguments[0], arguments[1]
	if role != "engine" && role != "child" && role != "grandchild" && role != "detached" && role != "unrelated" {
		return errors.New("unknown process fixture role")
	}
	lease, err := lifecycle.Acquire(filepath.Join(root, role))
	if err != nil {
		return err
	}
	defer lease.Close()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	start := func(next string, detached bool) error {
		cmd := exec.Command(self, "fixture", root, next)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if detached {
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	if role == "engine" {
		if err = start("child", false); err != nil {
			return err
		}
		if err = start("detached", true); err != nil {
			return err
		}
	} else if role == "child" {
		if err = start("grandchild", false); err != nil {
			return err
		}
	}
	if err = save(filepath.Join(root, role+".json"), participant{PID: os.Getpid(), Group: syscall.Getpgrp(), Root: root, Role: role}); err != nil {
		return err
	}
	// Even a crashed observer cannot leave these model-free fixtures indefinitely.
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = os.Stat(filepath.Join(root, "stop")); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func waitParticipants(root string) ([]participant, error) {
	values := []participant{}
	deadline := time.Now().Add(8 * time.Second)
	for _, role := range []string{"engine", "child", "grandchild", "detached"} {
		for {
			raw, err := os.ReadFile(filepath.Join(root, role+".json"))
			var value participant
			if err == nil && json.Unmarshal(raw, &value) == nil && value.Role == role && value.Root == root && value.PID > 1 {
				values = append(values, value)
				break
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("fixture did not become ready: %s", role)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return values, nil
}

func managed(arguments []string) error {
	flags := flag.NewFlagSet("managed", flag.ContinueOnError)
	root := flags.String("root", "", "owned probe installation")
	owner := flags.String("owner", "", "startup ownership")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := localnode.New(localnode.Options{Root: *root, Program: self})
	if err != nil {
		return err
	}
	var lifetime fixtureLifetime
	raw, err := os.ReadFile(filepath.Join(*root, "fixture-lifetime.json"))
	if err != nil || json.Unmarshal(raw, &lifetime) != nil || time.Now().After(lifetime.Until) {
		return nil // A stale temporary entry cannot keep restarting fixtures.
	}
	ctx, cancel := context.WithDeadline(context.Background(), lifetime.Until)
	defer cancel()
	return m.RunManaged(ctx, *owner, func(ctx context.Context, configured config.Config) error {
		generationRoot := filepath.Join(*root, "generations", protocol.ID())
		if lifetime.NativeEngine {
			if filepath.Dir(lifetime.Generation) != filepath.Join(*root, "generations") {
				return errors.New("native fixture generation left its installation")
			}
			generationRoot = lifetime.Generation
			intent, err := os.OpenFile(filepath.Join(*root, "native-start.intent"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if errors.Is(err, os.ErrExist) {
				return nil
			}
			if err != nil {
				return err
			}
			_, writeErr := intent.Write([]byte(protocol.Now()))
			if err = errors.Join(writeErr, intent.Sync(), intent.Close()); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(generationRoot, 0700); err != nil {
			return err
		}
		active := generation{PID: os.Getpid(), Group: syscall.Getpgrp(), Root: generationRoot}
		if lifetime.NativeEngine {
			execution, finish, err := startNativeFixture(ctx, configured, generationRoot, self)
			defer finish()
			if err != nil {
				_ = save(filepath.Join(generationRoot, "native-error.json"), map[string]any{"at": protocol.Now(), "error": err.Error()})
				return err
			}
			active.Execution = &execution
		} else {
			process, err := childprocess.StartContext(ctx, exec.Command(self, "fixture", generationRoot, "engine"))
			if err != nil {
				return err
			}
			defer func() { _ = process.Kill(); _ = process.Wait() }()
		}
		if _, err = waitParticipants(generationRoot); err != nil {
			return err
		}
		if err = save(filepath.Join(*root, "generation.json"), active); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	})
}

func held(root, role string) bool {
	lease, err := lifecycle.Acquire(filepath.Join(root, role))
	if err == nil {
		_ = lease.Close()
	}
	return err != nil
}

func nativePID(ctx context.Context, d startup.Definition) (int, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(ctx, "/bin/launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+d.Name)
	} else {
		args := []string{"show", d.Name + ".service", "--property=MainPID", "--value"}
		if os.Geteuid() != 0 {
			args = append([]string{"--user"}, args...)
		}
		cmd = exec.CommandContext(ctx, "systemctl", args...)
	}
	raw, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	if runtime.GOOS == "linux" {
		return strconv.Atoi(strings.TrimSpace(string(raw)))
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "pid = "); ok {
			return strconv.Atoi(value)
		}
	}
	return 0, errors.New("launchd did not expose an active main PID")
}

func clearTemporaryFailure(ctx context.Context, d startup.Definition) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	ctl := func(arguments ...string) *exec.Cmd {
		if os.Geteuid() != 0 {
			arguments = append([]string{"--user"}, arguments...)
		}
		return exec.CommandContext(ctx, "systemctl", arguments...)
	}
	raw, err := ctl("show", d.Name+".service", "--property=ActiveState", "--value").Output()
	if err != nil {
		return err
	}
	switch strings.TrimSpace(string(raw)) {
	case "inactive":
		return nil
	case "failed":
		return ctl("reset-failed", d.Name+".service").Run()
	default:
		return errors.New("temporary systemd entry is still active after cleanup")
	}
}

func probe() (resultErr error) {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	engineProgram := flags.String("engine", "", "optional verified native engine for real tool execution")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *engineProgram != "" && !filepath.IsAbs(*engineProgram) {
		return errors.New("choose an absolute project-local engine path")
	}
	parent, err := os.MkdirTemp(".state", "managed-exit-risk-")
	if err != nil {
		return err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return err
	}
	root := filepath.Join(parent, "device")
	if err = os.Mkdir(root, 0700); err != nil {
		return err
	}
	report := map[string]any{"at": protocol.Now(), "root": root, "platform": runtime.GOOS + "/" + runtime.GOARCH, "state": "starting", "modelEngineStarted": false, "modelCalls": 0, "sharedServicesChanged": false, "packagingValidation": false}
	if *engineProgram != "" {
		report["modelEngineStarted"], report["nativeEngineRequested"] = nil, true
	}
	reportFile := filepath.Join(parent, "report.json")
	if err = save(reportFile, report); err != nil {
		return err
	}
	fmt.Println("Managed owner-exit probe:", parent)
	self, err := os.Executable()
	if err != nil {
		return err
	}
	backend, err := startup.Native()
	if err != nil {
		return err
	}
	m, err := localnode.New(localnode.Options{Root: root, Program: self, Engine: *engineProgram})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	settings := &localnode.Setup{Name: "MewDex model-free managed owner-exit fixture", Hub: true, Listen: "127.0.0.1:0"}
	lifetime := fixtureLifetime{Until: time.Now().Add(35 * time.Second)}
	if *engineProgram != "" {
		settings.Executor = true
		settings.Projects = []protocol.Project{{ID: "process-fault", Name: "Owned process fault fixture", Path: filepath.Join(root, "generations")}}
		if err = os.MkdirAll(settings.Projects[0].Path, 0700); err != nil {
			return err
		}
		lifetime.NativeEngine, lifetime.Generation = true, filepath.Join(settings.Projects[0].Path, protocol.ID())
		lifetime.Until = time.Now().Add(75 * time.Second)
	}
	if err = save(filepath.Join(root, "fixture-lifetime.json"), lifetime); err != nil {
		return err
	}
	setup, err := m.Handle(ctx, localnode.Request{Operation: "setup", Setup: settings})
	if err != nil {
		return err
	}
	profile, err := os.ReadFile(filepath.Join(root, "device.json"))
	if err != nil {
		return err
	}
	binding := startup.Binding{DeviceID: setup.Snapshot.DeviceID, ProfileSHA256: startup.Fingerprint(profile)}
	program, err := runtimefiles.Program(root, self)
	if err != nil {
		return err
	}
	var provider *responsefixture.Fixture
	if *engineProgram != "" {
		providerRoot := filepath.Join(root, "provider")
		if err = os.Mkdir(providerRoot, 0700); err != nil {
			return err
		}
		fixture := responsefixture.New(providerRoot)
		provider = fixture
		fixture.Begin(quote(program) + " fixture " + quote(lifetime.Generation) + " engine")
		listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			return listenErr
		}
		server := &http.Server{Handler: fixture, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = server.Serve(listener) }()
		defer server.Close()
		var configured config.Config
		if err = json.Unmarshal(profile, &configured); err != nil {
			return err
		}
		if err = os.MkdirAll(configured.Engine.Home, 0700); err != nil {
			return err
		}
		text := fmt.Sprintf("model = \"gpt-6.1-sol\"\nmodel_provider = \"mewdex_fixture\"\nweb_search = \"disabled\"\n[model_providers.mewdex_fixture]\nname = \"MewDex deterministic loopback fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", "http://"+listener.Addr().String()+"/v1")
		if err = os.WriteFile(filepath.Join(configured.Engine.Home, "config.toml"), []byte(text), 0600); err != nil {
			return err
		}
	}
	log, err := localnode.OpenServiceLog(root)
	if err != nil {
		return err
	}
	if err = log.Close(); err != nil {
		return err
	}
	controller, err := startup.New(root, backend)
	if err != nil {
		return err
	}
	var registered startup.Record
	var unrelated *exec.Cmd
	defer func() {
		cleanupCtx, finish := context.WithTimeout(context.Background(), 25*time.Second)
		defer finish()
		var cleanupErr error
		if registered.Definition.Owner == "" {
			if raw, readErr := os.ReadFile(filepath.Join(root, "startup.json")); readErr == nil {
				_ = json.Unmarshal(raw, &registered)
			}
		}
		if registered.Definition.Owner != "" {
			_, disableErr := controller.Apply(cleanupCtx, false, "", binding)
			stopErr := backend.Stop(cleanupCtx, registered.Definition)
			cleanupErr = errors.Join(disableErr, stopErr)
			if cleanupErr == nil {
				cleanupErr = clearTemporaryFailure(cleanupCtx, registered.Definition)
			}
		}
		if provider != nil {
			report["loopbackProvider"] = provider.Snapshot()
		}
		// Mark only our bounded fixture generations for cooperative cleanup.
		// This is AFTER recording the OS cleanup result, never a passing condition.
		generations, _ := os.ReadDir(filepath.Join(root, "generations"))
		for _, entry := range generations {
			if entry.IsDir() {
				cleanupErr = errors.Join(cleanupErr, os.WriteFile(filepath.Join(root, "generations", entry.Name(), "stop"), nil, 0600))
			}
		}
		if unrelated != nil {
			cleanupErr = errors.Join(cleanupErr, os.WriteFile(filepath.Join(parent, "unrelated", "stop"), nil, 0600), unrelated.Wait())
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			remaining := false
			for _, entry := range generations {
				if !entry.IsDir() {
					continue
				}
				for _, role := range []string{"engine", "child", "grandchild", "detached"} {
					remaining = held(filepath.Join(root, "generations", entry.Name()), role) || remaining
				}
			}
			if !remaining {
				report["fixturesStoppedAfterProbeCleanup"] = true
				break
			}
			if time.Now().After(deadline) {
				cleanupErr = errors.Join(cleanupErr, errors.New("bounded fixture cleanup is unconfirmed"))
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if registered.Definition.Owner != "" {
			entry, inspectErr := backend.Inspect(cleanupCtx, registered.Definition)
			if inspectErr != nil || entry.Present || entry.Running {
				cleanupErr = errors.Join(cleanupErr, errors.New("temporary native entry cleanup is unconfirmed"))
			}
		}
		if lifetime.NativeEngine {
			runtimeErr := cleanOwnedRuntimes(cleanupCtx, root)
			report["privateNativeRuntimesCleaned"] = runtimeErr == nil
			cleanupErr = errors.Join(cleanupErr, runtimeErr)
		}
		report["cleanupConfirmed"] = cleanupErr == nil
		resultErr = errors.Join(resultErr, cleanupErr)
		report["state"] = "succeeded"
		if resultErr != nil {
			report["state"], report["error"] = "failed", resultErr.Error()
		}
		report["finishedAt"] = protocol.Now()
		_ = save(reportFile, report)
		raw, _ := json.Marshal(report)
		fmt.Println(string(raw))
	}()
	if _, err = controller.Apply(ctx, true, program, binding); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(root, "startup.json"))
	if err != nil || json.Unmarshal(raw, &registered) != nil {
		return errors.New("startup registration was not recorded")
	}
	report["nativeDefinition"] = registered.Definition
	if err = controller.Start(ctx, binding); err != nil {
		return err
	}
	var active generation
	deadline := time.Now().Add(40 * time.Second)
	for {
		raw, err = os.ReadFile(filepath.Join(root, "generation.json"))
		if err == nil && json.Unmarshal(raw, &active) == nil && active.PID > 1 && filepath.Dir(active.Root) == filepath.Join(root, "generations") {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("managed service did not start its isolated process fixture")
		}
		time.Sleep(25 * time.Millisecond)
	}
	participants, err := waitParticipants(active.Root)
	if err != nil {
		return err
	}
	for _, value := range participants {
		if !held(active.Root, value.Role) {
			return errors.New("fixture was not alive before the fault")
		}
	}
	if err = os.Mkdir(filepath.Join(parent, "unrelated"), 0700); err != nil {
		return err
	}
	unrelated = exec.Command(self, "fixture", filepath.Join(parent, "unrelated"), "unrelated")
	unrelated.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = unrelated.Start(); err != nil {
		unrelated = nil
		return err
	}
	deadline = time.Now().Add(3 * time.Second)
	for !held(filepath.Join(parent, "unrelated"), "unrelated") {
		if time.Now().After(deadline) {
			return errors.New("unrelated sentinel was not ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	actualPID, err := nativePID(ctx, registered.Definition)
	if err != nil || actualPID != active.PID {
		return errors.New("OS manager did not confirm the current owned main PID")
	}
	report["generation"], report["participants"] = active, participants
	if active.Execution != nil {
		report["modelEngineStarted"] = true
		report["nativeExecution"] = active.Execution
	}
	report["state"], report["fault"] = "observing", "SIGKILL of OS-confirmed owned main PID only"
	if err = save(reportFile, report); err != nil {
		return err
	}
	if err = syscall.Kill(active.PID, syscall.SIGKILL); err != nil {
		return err
	}
	remaining := map[string]bool{}
	var runtimes []runtimeProcess
	deadline = time.Now().Add(3 * time.Second)
	for {
		anyHeld := false
		for _, value := range participants {
			remaining[value.Role] = held(active.Root, value.Role)
			anyHeld = anyHeld || remaining[value.Role]
		}
		if active.Execution != nil {
			runtimes, err = ownedRuntimes(ctx, root)
			if err != nil {
				return err
			}
			anyHeld = anyHeld || len(runtimes) > 0
		}
		if !anyHeld || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	report["heldAfterOwnerExit"] = remaining
	if active.Execution != nil {
		report["ownedNativeRuntimesAfterOwnerExit"] = runtimes
	}
	report["unrelatedPreserved"] = held(filepath.Join(parent, "unrelated"), "unrelated")
	for _, value := range remaining {
		if value {
			return errors.New("an owned process survived the managed service's forced exit")
		}
	}
	if len(runtimes) != 0 {
		return errors.New("a private native runtime survived its service owner")
	}
	if report["unrelatedPreserved"] != true {
		return errors.New("owner exit affected the unrelated sentinel")
	}
	return nil
}
