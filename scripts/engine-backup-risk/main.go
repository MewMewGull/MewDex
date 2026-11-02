// Read a previously completed unsigned probe, archive its owned state, and load
// that history from a fresh home. No new turn, sign-in or tool is permitted.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"mewdex/internal/backup"
	"mewdex/internal/config"
	"mewdex/internal/engine"
	"mewdex/internal/executor"
	"mewdex/internal/protocol"
	"mewdex/internal/store"
)

type observationOnly struct {
	engine.Engine
	starts atomic.Int32
}

func (e *observationOnly) Call(ctx context.Context, method string, params, out any) error {
	if method == "turn/start" || method == "turn/steer" {
		e.starts.Add(1)
		return errors.New("restored-history probe does not authorize new input")
	}
	return e.Engine.Call(ctx, method, params, out)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "AZURE_OPENAI_API_KEY", "OPENAI_BASE_URL", "CHATGPT_BASE_URL"} {
		if os.Getenv(key) != "" {
			return errors.New("model credential or endpoint overrides are present; history probe was not started")
		}
	}
	raw, err := os.ReadFile(".state/evidence/engine-execution-recovery.json")
	if err != nil {
		return err
	}
	var prior struct {
		ProbeDirectory string                        `json:"probeDirectory"`
		Isolated       bool                          `json:"isolatedUnsignedAccount"`
		Observation    protocol.ExecutionObservation `json:"observation"`
	}
	if err = json.Unmarshal(raw, &prior); err != nil || !prior.Isolated {
		return errors.New("existing evidence is not an unsigned execution probe")
	}
	source, err := filepath.EvalSymlinks(prior.ProbeDirectory)
	if err != nil {
		return err
	}
	state, err := filepath.Abs(".state")
	if err != nil {
		return err
	}
	state, err = filepath.EvalSymlinks(state)
	if err != nil {
		return err
	}
	if filepath.Dir(source) != state || !strings.HasPrefix(filepath.Base(source), "engine-execution-") {
		return errors.New("source must be an existing project-local execution probe")
	}
	root, err := os.MkdirTemp(state, "engine-backup-risk-")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	selections := []backup.Selection{{Path: "engine-home/sessions", Name: "engine-home/sessions"}}
	for _, name := range []string{"executor.db", "executor.db-wal", "executor.db-shm"} {
		if _, err := os.Stat(filepath.Join(source, name)); err == nil {
			selections = append(selections, backup.Selection{Path: name, Name: "data/" + name})
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	archive := filepath.Join(root, "device.mewdex-backup")
	password := protocol.ID() + protocol.ID() // Retained only for this in-memory probe.
	manifest, err := backup.Create(ctx, source, archive, password, json.RawMessage(`{"probe":true}`), selections, backup.Options{IncludeFile: func(name string) bool {
		return !strings.HasPrefix(name, "engine-home/") || engine.IsHistoryBackupFile(strings.TrimPrefix(name, "engine-home/"))
	}})
	if err != nil {
		return err
	}
	if _, err = backup.Extract(ctx, archive, password, root, "restored", backup.Limits{}); err != nil {
		return err
	}
	data := filepath.Join(root, "restored", "data")
	s, err := store.Open(filepath.Join(data, "executor.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	sessions, err := store.List[protocol.Session](s, "sessions")
	if err != nil {
		return err
	}
	if len(sessions) != 1 || sessions[0].DeviceID != "isolated-risk" || sessions[0].LastStartID == "" {
		return errors.New("restored state is not the expected execution probe")
	}
	before := sessions[0]
	receipt, err := s.Result(before.LastStartID)
	if err != nil {
		return err
	}
	home := filepath.Join(root, "restored", "engine-home")
	executable, err := filepath.Abs("apps/client/src-tauri/engine/bin/codex.exe")
	if err != nil {
		return err
	}
	real, err := engine.Start(ctx, config.Engine{Executable: executable, Home: home})
	if err != nil {
		return err
	}
	var account struct {
		Account json.RawMessage `json:"account"`
	}
	if err = real.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		real.Close()
		return err
	}
	if len(account.Account) > 0 && string(account.Account) != "null" {
		real.Close()
		return errors.New("restored home unexpectedly has a model account")
	}
	wrapped := &observationOnly{Engine: real}
	x, err := executor.New(config.Config{DeviceID: "isolated-risk", DataDir: data, Projects: []protocol.Project{{ID: "risk", Path: filepath.Join(source, "workspace")}}}, s, wrapped, nil)
	if err != nil {
		real.Close()
		return err
	}
	defer x.Close()
	result := x.Handle(ctx, protocol.Command{ID: protocol.ID(), DeviceID: before.DeviceID, SessionID: before.ID, Kind: "reconcile", SourceID: "backup-risk-input"})
	if result.State != "completed" {
		return fmt.Errorf("restored history could not be observed: %s", result.Error)
	}
	var observed protocol.ExecutionObservation
	if err = json.Unmarshal(result.Data, &observed); err != nil {
		return err
	}
	if !observed.Matched || observed.ClientID != before.LastStartID || observed.ClientID != prior.Observation.ClientID || observed.TurnID != prior.Observation.TurnID || observed.TurnStatus != prior.Observation.TurnStatus || observed.TurnStatus != "failed" {
		_ = os.WriteFile(".state/evidence/engine-backup-history-diagnostic.json", protocol.JSON(map[string]any{"root": root, "storedTurnId": before.TurnID, "storedClientId": before.LastStartID, "observation": observed}), 0600)
		return errors.New("restored history did not match its original task")
	}
	after, err := s.Result(before.LastStartID)
	if err != nil {
		return err
	}
	if after.State != receipt.State || wrapped.starts.Load() != 0 {
		return errors.New("restoring history changed or resubmitted the original command")
	}
	report := map[string]any{"at": protocol.Now(), "root": root, "files": len(manifest.Files), "freshHome": true, "modelAccountImported": false, "engineThreadPreserved": true, "originalClientIDMatched": true, "originalTurnIDMatched": true, "originalReceiptPreserved": true, "turnStartCalls": wrapped.starts.Load(), "inferenceRun": false, "toolsAuthorized": false, "finalCandidate": false, "profileSwitched": false}
	if err = os.WriteFile(".state/evidence/engine-backup-history.json", protocol.JSON(report), 0600); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
