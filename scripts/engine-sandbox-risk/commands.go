package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"mewdex/internal/engine"
	"mewdex/internal/protocol"
)

func child(operation, target string) {
	var err error
	value := map[string]any{"operation": operation}
	switch operation {
	case "read":
		var data []byte
		data, err = os.ReadFile(target)
		if err == nil {
			value["sha256"] = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	case "write":
		err = os.WriteFile(target, []byte("mewdex-command-write"), 0600)
	case "network":
		var connection net.Conn
		connection, err = net.DialTimeout("tcp", target, 750*time.Millisecond)
		if err == nil {
			connection.Close()
		}
	case "move-read":
		var paths []string
		if err = json.Unmarshal([]byte(target), &paths); err == nil && len(paths) == 3 {
			err = os.Rename(paths[0], paths[1])
			if err == nil {
				value["directoryMoved"] = true
				var data []byte
				data, err = os.ReadFile(filepath.Join(paths[1], paths[2]))
				if err == nil {
					value["sha256"] = fmt.Sprintf("%x", sha256.Sum256(data))
				}
			}
		} else {
			err = errors.New("invalid controlled move probe")
		}
	default:
		err = errors.New("unknown fixed probe operation")
	}
	value["allowed"] = err == nil
	if err != nil {
		value["permissionDenied"] = os.IsPermission(err)
		value["error"] = err.Error()
	}
	fmt.Println(string(protocol.JSON(value)))
}

type commandProbePaths struct {
	Root, Workspace, Inputs, Private, ProfileID string
	StagedInput, SourceAttachment               string
	MoveAncestor                                bool
}

func runCommands(ctx context.Context, e *engine.Codex, paths commandProbePaths, report map[string]any) error {
	root, workspace, private := paths.Root, paths.Workspace, paths.Private
	program, err := os.Executable()
	if err != nil {
		return err
	}
	inside, outside, input := filepath.Join(workspace, "inside.txt"), filepath.Join(root, "outside.txt"), paths.StagedInput
	fixtures := []string{inside, outside}
	if input == "" {
		input = filepath.Join(paths.Inputs, "input.txt")
		fixtures = append(fixtures, input)
	}
	for _, path := range fixtures {
		if err = os.WriteFile(path, []byte("mewdex-preserve-marker"), 0600); err != nil {
			return err
		}
	}
	link := filepath.Join(workspace, "outside-link")
	if err = os.Symlink(outside, link); err != nil {
		return err
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
	accepted, err := listener.Accept()
	if err != nil {
		return err
	}
	accepted.Close()
	report["networkListenerPositiveControl"] = true
	tests := []struct {
		name, operation, path string
		allowed               bool
	}{
		{"read-project", "read", inside, true},
		{"write-project", "write", filepath.Join(workspace, "written.txt"), true},
		{"read-input", "read", input, true},
		{"read-outside", "read", outside, false},
		{"write-outside", "write", outside, false},
		{"read-project-link-outside", "read", link, false},
		{"write-project-link-outside", "write", link, false},
		{"write-input", "write", input, false},
		{"network-disabled", "network", listener.Addr().String(), false},
	}
	if private != "" {
		privateFile := filepath.Join(private, "private.txt")
		if err = os.WriteFile(privateFile, []byte("mewdex-preserve-marker"), 0600); err != nil {
			return err
		}
		tests = append(tests, struct {
			name, operation, path string
			allowed               bool
		}{"read-private-inside-project", "read", privateFile, false}, struct {
			name, operation, path string
			allowed               bool
		}{"write-private-inside-project", "write", privateFile, false})
		report["privateRootInsideWorkspace"] = private
	}
	if paths.SourceAttachment != "" {
		tests = append(tests, struct {
			name, operation, path string
			allowed               bool
		}{"read-original-private-attachment", "read", paths.SourceAttachment, false}, struct {
			name, operation, path string
			allowed               bool
		}{"write-original-private-attachment", "write", paths.SourceAttachment, false})
	}
	results := []map[string]any{}
	for _, test := range tests {
		result := map[string]any{"name": test.name, "expectedAllowed": test.allowed}
		results = append(results, result)
		report["commandResults"] = results
		report["commandsAttempted"] = len(results)
		if err = os.WriteFile(filepath.Join(root, "inspection.json"), protocol.JSON(report), 0600); err != nil {
			return err
		}
		var response struct {
			ExitCode int    `json:"exitCode"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
		}
		err = e.Call(ctx, "command/exec", map[string]any{"command": []string{program, "-sandbox-child", test.operation, test.path}, "cwd": workspace, "permissionProfile": paths.ProfileID, "timeoutMs": 10000, "outputBytesCap": 8192}, &response)
		if err != nil {
			result["rpcError"] = err.Error()
			return err
		}
		result["exitCode"], result["stderr"] = response.ExitCode, response.Stderr
		var observation struct {
			Allowed          bool   `json:"allowed"`
			PermissionDenied bool   `json:"permissionDenied"`
			SHA256           string `json:"sha256"`
		}
		if err = json.Unmarshal([]byte(response.Stdout), &observation); err != nil || response.ExitCode != 0 {
			result["stdout"] = response.Stdout
			return errors.New("sandbox did not return a valid child observation")
		}
		result["allowed"], result["permissionDenied"] = observation.Allowed, observation.PermissionDenied
		report["commandsExecuted"] = len(results)
		if test.operation == "write" {
			// Bubblewrap may allow a write into a disposable namespace ancestor.
			// The boundary protects host files; verify persistence on the host.
			hostData, hostErr := os.ReadFile(test.path)
			persisted := hostErr == nil && string(hostData) == "mewdex-command-write"
			result["hostWritePersisted"] = persisted
			if persisted != test.allowed || (test.allowed && !observation.Allowed) {
				return fmt.Errorf("host write permission mismatch: %s", test.name)
			}
		} else if observation.Allowed != test.allowed {
			return fmt.Errorf("permission mismatch: %s", test.name)
		}
		if test.operation == "read" && test.allowed && observation.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("mewdex-preserve-marker"))) {
			return fmt.Errorf("project/input read did not match its fixture: %s", test.name)
		}
	}
	if paths.MoveAncestor {
		if err = probeAncestorMove(ctx, e, program, paths, report); err != nil {
			return err
		}
	}
	preserved := []string{outside, input}
	if private != "" {
		preserved = append(preserved, filepath.Join(private, "private.txt"))
	}
	if paths.SourceAttachment != "" {
		preserved = append(preserved, paths.SourceAttachment)
	}
	for _, path := range preserved {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "mewdex-preserve-marker" {
			return errors.New("an out-of-workspace probe fixture was changed")
		}
	}
	report["outsideAndInputPreserved"] = true
	return nil
}

func probeAncestorMove(ctx context.Context, e *engine.Codex, program string, paths commandProbePaths, report map[string]any) error {
	source := filepath.Dir(paths.Private)
	target := filepath.Join(paths.Workspace, "moved-owned-container")
	if filepath.Dir(source) != paths.Workspace || source == paths.Workspace {
		return errors.New("move probe is not bounded to its owned workspace")
	}
	prior, err := os.Stat(source)
	if err != nil {
		return err
	}
	results := report["commandResults"].([]map[string]any)
	result := map[string]any{"name": "rename-private-ancestor-and-read", "expectedAllowed": false}
	results = append(results, result)
	report["commandResults"], report["commandsAttempted"] = results, len(results)
	if err = os.WriteFile(filepath.Join(paths.Root, "inspection.json"), protocol.JSON(report), 0600); err != nil {
		return err
	}
	var response struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	arguments := string(protocol.JSON([]string{source, target, filepath.Join(filepath.Base(paths.Private), "private.txt")}))
	err = e.Call(ctx, "command/exec", map[string]any{"command": []string{program, "-sandbox-child", "move-read", arguments}, "cwd": paths.Workspace, "permissionProfile": paths.ProfileID, "timeoutMs": 10000, "outputBytesCap": 8192}, &response)
	// Any successful rename only moves this probe's verified fixture. Restore
	// its exact identity before checking the result; never move an unknown dir.
	if moved, statErr := os.Stat(target); statErr == nil {
		if !os.SameFile(prior, moved) {
			return errors.New("move probe destination has an unexpected identity")
		}
		if _, sourceErr := os.Lstat(source); !errors.Is(sourceErr, os.ErrNotExist) {
			return errors.New("move probe source changed unexpectedly")
		}
		if restoreErr := os.Rename(target, source); restoreErr != nil {
			return restoreErr
		}
		result["hostFixtureRestored"] = true
	}
	if err != nil {
		result["rpcError"] = err.Error()
		return err
	}
	var observation struct {
		Allowed bool   `json:"allowed"`
		Moved   bool   `json:"directoryMoved"`
		SHA     string `json:"sha256"`
	}
	if err = json.Unmarshal([]byte(response.Stdout), &observation); err != nil || response.ExitCode != 0 {
		result["stdout"], result["stderr"] = response.Stdout, response.Stderr
		return errors.New("move probe did not return a valid observation")
	}
	result["allowed"], result["directoryMoved"], result["sha256"] = observation.Allowed, observation.Moved, observation.SHA
	report["commandsExecuted"] = len(results)
	if observation.Allowed {
		return errors.New("moving a private ancestor exposed its fixture; protect the top project segment before integrating execution")
	}
	return nil
}
