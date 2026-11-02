package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
)

// This mode only uses the already prepared, approved home and workspace.
// It cannot invoke setup, choose another account home, or submit a model turn.
func probePreparedWindows(path string) (resultErr error) {
	if runtime.GOOS != "windows" {
		return errors.New("Windows only")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	frozenHelper := filepath.Join(filepath.Dir(filepath.Dir(absolute)), "dev", "mewdex-engine-sandbox-setup-v2.exe")
	plan, err := validateWindowsPlan(absolute, frozenHelper, true)
	if err != nil {
		return err
	}
	root, err := os.MkdirTemp(plan.Workspace, "commands-risk-")
	if err != nil {
		return err
	}
	report := map[string]any{"at": protocol.Now(), "root": root, "preparedHome": plan.Home, "engineVersion": plan.EngineVersion, "setupRequested": false, "modelTurnsSubmitted": 0, "finalAcceptance": false}
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
	helper, err := os.Executable()
	if err != nil {
		return err
	}
	program, err := runtimefiles.Program(root, helper)
	if err != nil {
		return err
	}
	private, err := os.MkdirTemp(plan.Root, "command-private-")
	if err != nil {
		return err
	}
	inside, outside := filepath.Join(root, "inside.txt"), filepath.Join(private, "outside.txt")
	const marker = "mewdex-preserve-marker"
	for _, file := range []string{inside, outside} {
		if err = os.WriteFile(file, []byte(marker), 0600); err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	control, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		return err
	}
	control.Close()
	report["networkPositiveControl"] = true
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	e, err := startWindowsCandidate(ctx, plan)
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
		for n := range e.Notifications() {
			if len(n.RequestID) > 0 {
				_ = e.Reject(ctx, n.RequestID, "Only fixed Windows command probes are authorized")
			}
		}
	}()
	var readiness struct {
		Status string `json:"status"`
	}
	if err = e.Call(ctx, "windowsSandbox/readiness", nil, &readiness); err != nil {
		return err
	}
	report["readiness"] = readiness.Status
	if readiness.Status != "ready" {
		return errors.New("the approved home is not ready; observe without repeating setup")
	}
	tests := []struct {
		name, operation, target string
		allowed                 bool
	}{
		{"read-workspace", "read", inside, true},
		{"write-workspace", "write", filepath.Join(root, "written.txt"), true},
		{"read-outside", "read", outside, false},
		{"write-outside", "write", outside, false},
		{"network-disabled", "network", listener.Addr().String(), false},
	}
	results := []map[string]any{}
	for _, test := range tests {
		result := map[string]any{"name": test.name, "expectedAllowed": test.allowed}
		results = append(results, result)
		report["commands"] = results
		if err = os.WriteFile(filepath.Join(root, "inspection.json"), protocol.JSON(report), 0600); err != nil {
			return err
		}
		var response struct {
			ExitCode int    `json:"exitCode"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
		}
		// The pinned Windows sandbox rejects a custom outputBytesCap. Each
		// fixed child emits one bounded observation and never arbitrary output.
		if err = e.Call(ctx, "command/exec", map[string]any{"command": []string{program, "-sandbox-child", test.operation, test.target}, "cwd": root, "permissionProfile": "mewdex-probe", "timeoutMs": 10000}, &response); err != nil {
			result["rpcError"] = err.Error()
			return err
		}
		var observation struct {
			Allowed          bool `json:"allowed"`
			PermissionDenied bool `json:"permissionDenied"`
		}
		if response.ExitCode != 0 || json.Unmarshal([]byte(response.Stdout), &observation) != nil {
			result["stdout"], result["stderr"] = response.Stdout, response.Stderr
			return errors.New("Windows command returned no valid fixed observation")
		}
		result["allowed"], result["permissionDenied"] = observation.Allowed, observation.PermissionDenied
		if observation.Allowed != test.allowed {
			return fmt.Errorf("Windows permission mismatch: %s", test.name)
		}
		if test.operation == "write" {
			actual, readErr := os.ReadFile(test.target)
			persisted := readErr == nil && string(actual) == "mewdex-command-write"
			result["hostWritePersisted"] = persisted
			if persisted != test.allowed {
				return errors.New("Windows host write did not match its sandbox result")
			}
		}
	}
	actual, err := os.ReadFile(outside)
	if err != nil || string(actual) != marker {
		return errors.New("outside fixture was not preserved")
	}
	report["outsidePreserved"] = true
	return nil
}
