// startup-risk registers only a temporary, isolated Hub-only installation.
// Its OS entry is removed before completion; no model engine or login is used.
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
	"time"

	"mewdex/internal/localnode"
	"mewdex/internal/startup"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (resultErr error) {
	binaryFlag := flag.String("binary", "", "new development helper executable")
	flag.Parse()
	binary, err := filepath.Abs(*binaryFlag)
	if err != nil || *binaryFlag == "" {
		return errors.New("choose the development helper")
	}
	parent, err := os.MkdirTemp(".state", "startup-risk-")
	if err != nil {
		return err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return err
	}
	root := filepath.Join(parent, "设备 服务")
	if err = os.Mkdir(root, 0700); err != nil {
		return err
	}
	backend, err := startup.Native()
	if err != nil {
		return err
	}
	m, err := localnode.New(localnode.Options{Root: root, Program: binary})
	if err != nil {
		return err
	}
	report := map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "platform": runtime.GOOS + "/" + runtime.GOARCH, "root": root, "scope": backend.Scope(), "modelEngineStarted": false, "inferenceRun": false, "sharedServicesChanged": false, "finalCandidate": false, "state": "running"}
	reportPath := filepath.Join(parent, "report.json")
	save := func() error {
		raw, _ := json.MarshalIndent(report, "", "  ")
		return os.WriteFile(reportPath, raw, 0600)
	}
	if err = save(); err != nil {
		return err
	}
	fmt.Println("Isolated startup probe:", parent)
	var deviceID string
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		var cleanupErr error
		if deviceID != "" {
			_, stopErr := m.Handle(ctx, localnode.Request{Operation: "stop", ExpectedDeviceID: deviceID})
			var registration startup.Record
			if raw, readErr := os.ReadFile(filepath.Join(root, "startup.json")); readErr == nil && json.Unmarshal(raw, &registration) == nil {
				if stopErr != nil {
					stopErr = backend.Stop(ctx, registration.Definition)
				}
			}
			_, removeErr := m.Handle(ctx, localnode.Request{Operation: "startup.apply", ExpectedDeviceID: deviceID, Startup: &localnode.StartupRequest{Enabled: false}})
			if registration.Definition.Owner != "" {
				removeErr = errors.Join(removeErr, backend.Stop(ctx, registration.Definition))
			}
			cleanupErr = errors.Join(stopErr, removeErr)
			resultErr = errors.Join(resultErr, cleanupErr)
		}
		report["state"] = "succeeded"
		if resultErr != nil {
			report["state"] = "failed"
			report["error"] = resultErr.Error()
		}
		report["cleanupConfirmed"] = cleanupErr == nil
		_ = save()
		if resultErr == nil {
			raw, _ := json.Marshal(report)
			fmt.Println(string(raw))
		}
	}()
	call := func(request localnode.Request) (localnode.Result, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		request.ExpectedDeviceID = deviceID
		return m.Handle(ctx, request)
	}
	configured, err := call(localnode.Request{Operation: "setup", Setup: &localnode.Setup{Name: "MewDex isolated startup probe", Hub: true, Listen: "127.0.0.1:0"}})
	if err != nil {
		return err
	}
	deviceID = configured.Snapshot.DeviceID
	enabled, err := call(localnode.Request{Operation: "startup.apply", Startup: &localnode.StartupRequest{Enabled: true}})
	if err != nil {
		return err
	}
	if enabled.Startup == nil || !enabled.Startup.Enabled || !enabled.Startup.EntryMatches || !enabled.Startup.MatchesProfile || enabled.Snapshot.State != "stopped" {
		return errors.New("startup registration did not stay stopped or match its device")
	}
	report["registeredWithoutStarting"] = true
	started, err := call(localnode.Request{Operation: "start"})
	if err != nil {
		return err
	}
	if !started.Snapshot.Ready || started.Snapshot.InstanceID == "" {
		return errors.New("registered service was not ready")
	}
	report["startedThroughOSManager"] = true
	stopped, err := call(localnode.Request{Operation: "stop", ExpectedInstanceID: started.Snapshot.InstanceID})
	if err != nil || stopped.Snapshot.State != "stopped" {
		return errors.New("registered service stop was not confirmed")
	}
	time.Sleep(2 * time.Second)
	observed, err := call(localnode.Request{Operation: "status"})
	if err != nil || observed.Snapshot.State != "stopped" {
		return errors.New("intentional stop did not remain stopped during observation")
	}
	report["intentionalStopObserved"] = true
	again, err := call(localnode.Request{Operation: "start"})
	if err != nil || !again.Snapshot.Ready || again.Snapshot.InstanceID == started.Snapshot.InstanceID {
		return errors.New("registered service could not start again")
	}
	disabled, err := call(localnode.Request{Operation: "startup.apply", Startup: &localnode.StartupRequest{Enabled: false}})
	if err != nil {
		return err
	}
	if disabled.Startup == nil || disabled.Startup.Registered || disabled.Startup.RequestedEnabled || !disabled.Snapshot.Ready || disabled.Snapshot.InstanceID != again.Snapshot.InstanceID {
		return errors.New("removing future startup stopped the existing service or retained registration")
	}
	report["disablePreservedRunningService"] = true
	return nil
}
