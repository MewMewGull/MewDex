// Observe an owned unsigned probe through the newly pinned native engine.
// Only the selected rollout file is copied. No model input or login is sent.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executionscope"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	program := flag.String("program", "", "project-local pinned native engine")
	priorPath := flag.String("prior", "", "unsigned native probe report")
	flag.Parse()
	state, err := filepath.Abs(".state")
	if err != nil {
		return err
	}
	state, err = filepath.EvalSymlinks(state)
	if err != nil {
		return err
	}
	owned := func(path string) (string, error) {
		path, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(state, path)
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			return "", errors.New("probe paths must stay in this project's .state directory")
		}
		return path, nil
	}
	*program, err = owned(*program)
	if err != nil {
		return err
	}
	runtimePaths, err := runtimefiles.EngineResources(*program, engine.CompatibleVersion)
	if err != nil {
		return err
	}
	*priorPath, err = owned(*priorPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(*priorPath)
	if err != nil {
		return err
	}
	var prior struct {
		EngineVersion string `json:"engineVersion"`
		Home          string `json:"preparedWindowsHome"`
		RealAccount   bool   `json:"realAccountUsed"`
		Inference     int    `json:"officialInferenceRequests"`
		State         string `json:"state"`
		Phases        []struct {
			EngineID string `json:"engineId"`
			TurnID   string `json:"turnId"`
		} `json:"phases"`
	}
	if json.Unmarshal(raw, &prior) != nil || prior.EngineVersion != "0.154.0" || prior.RealAccount || prior.Inference != 0 || prior.State != "succeeded" || len(prior.Phases) != 1 || prior.Phases[0].EngineID == "" || prior.Phases[0].TurnID == "" {
		return errors.New("source is not the expected completed unsigned 0.154.0 probe")
	}
	home, err := owned(prior.Home)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(filepath.Join(home, "auth.json")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("source home has an authentication file or cannot be checked")
	}
	threadID, turnID := prior.Phases[0].EngineID, prior.Phases[0].TurnID
	var selected string
	err = filepath.WalkDir(home, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		if entry.IsDir() && rel != "." && rel != "sessions" && rel != "archived_sessions" && !strings.HasPrefix(filepath.ToSlash(rel), "sessions/") && !strings.HasPrefix(filepath.ToSlash(rel), "archived_sessions/") {
			return filepath.SkipDir
		}
		if !entry.Type().IsRegular() || !strings.Contains(entry.Name(), threadID) || !engine.IsHistoryBackupFile(filepath.ToSlash(rel)) {
			return nil
		}
		if selected != "" {
			return errors.New("source has multiple matching rollout files")
		}
		selected = rel
		return nil
	})
	if err != nil {
		return err
	}
	if selected == "" {
		return errors.New("selected source rollout is missing")
	}
	root, err := os.MkdirTemp(state, "history-migration-risk-")
	if err != nil {
		return err
	}
	newHome := filepath.Join(root, "engine-home")
	source, err := os.OpenRoot(home)
	if err != nil {
		return err
	}
	defer source.Close()
	history, err := source.ReadFile(selected)
	if err != nil {
		return err
	}
	if len(history) > 20<<20 {
		return errors.New("owned probe history is unexpectedly large")
	}
	destination := filepath.Join(newHome, selected)
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(destination, history, 0600); err != nil {
		return err
	}
	digest := sha256.Sum256(history)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	native, err := engine.Start(ctx, config.Engine{Executable: *program, Home: newHome})
	if err != nil {
		return err
	}
	defer native.Close()
	go func() {
		for range native.Notifications() {
		}
	}()
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if err = native.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		return err
	}
	if len(account.Account) > 0 && string(account.Account) != "null" {
		return errors.New("new probe home unexpectedly has a model account")
	}
	workspace := filepath.Join(root, "workspace")
	if err = os.Mkdir(workspace, 0700); err != nil {
		return err
	}
	profile, err := executionscope.Build(executionscope.Spec{DeviceID: "history-risk", ProjectID: "history-risk", EngineVersion: engine.CompatibleVersion, Workspace: workspace, ReadOnly: runtimePaths, Private: []string{newHome}})
	if err != nil {
		return err
	}
	var resumed json.RawMessage
	if err = native.Call(ctx, "thread/resume", map[string]any{"threadId": threadID, "excludeTurns": true, "cwd": workspace, "modelProvider": "openai", "permissions": profile.ID(), "config": profile.ThreadConfig(), "approvalPolicy": "on-request", "approvalsReviewer": "user"}, &resumed); err != nil {
		return err
	}
	if err = profile.VerifyThread(resumed); err != nil {
		return err
	}
	var metadata struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err = native.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &metadata); err != nil {
		return err
	}
	if metadata.Thread.ID != threadID {
		return errors.New("native history changed the source thread ID")
	}
	var turns struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Items  []struct {
				Type     string `json:"type"`
				ClientID string `json:"clientId"`
			} `json:"items"`
		} `json:"data"`
	}
	if err = native.Call(ctx, "thread/turns/list", map[string]any{"threadId": threadID, "limit": 50, "itemsView": "summary"}, &turns); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "turns-read.json"), protocol.JSON(turns), 0600); err != nil {
		return err
	}
	matched, clientID := false, ""
	for _, turn := range turns.Data {
		if turn.ID == turnID {
			matched = turn.Status == "completed"
			for _, item := range turn.Items {
				if item.Type == "userMessage" {
					clientID = item.ClientID
				}
			}
		}
	}
	if !matched || clientID == "" {
		return errors.New("native history did not preserve completed turn and original client ID")
	}
	after, err := source.ReadFile(selected)
	if err != nil || sha256.Sum256(after) != digest {
		return errors.New("source rollout changed during observation")
	}
	if err = native.Close(); err != nil {
		return err
	}
	report := map[string]any{"at": protocol.Now(), "root": root, "sourceVersion": prior.EngineVersion, "targetVersion": engine.CompatibleVersion, "threadId": threadID, "turnId": turnID, "clientId": clientID, "sourceSHA256": hex.EncodeToString(digest[:]), "sourceUnchanged": true, "freshUnsignedHome": true, "copiedRollouts": 1, "nativeMetadataRead": true, "nativeTurnReceiptRead": true, "officialInferenceRequests": 0, "newTurns": 0, "liveServicesUpdated": false, "packagingValidation": false}
	if err = os.WriteFile(filepath.Join(root, "report.json"), protocol.JSON(report), 0600); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
