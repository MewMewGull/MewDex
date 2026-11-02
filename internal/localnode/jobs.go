package localnode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"mewdex/internal/lifecycle"
	"mewdex/internal/protocol"
	"mewdex/internal/runtimefiles"
)

// A job persists its intent and observations, never the request containing the
// archive passphrase. A detached worker receives that request over stdin once.
type jobIntent struct {
	Operation        string `json:"operation"`
	ExpectedDeviceID string `json:"expectedDeviceId,omitempty"`
	Path             string `json:"path,omitempty"`
	RestoreID        string `json:"restoreId,omitempty"`
	UpgradeID        string `json:"upgradeId,omitempty"`
}

type jobRecord struct {
	Format int       `json:"format"`
	Intent jobIntent `json:"intent"`
	Info   JobInfo   `json:"info"`
}

func intentFor(request *JobRequest) (jobIntent, error) {
	if request == nil || !localID(request.ID) {
		return jobIntent{}, errors.New("a stable local operation ID is required")
	}
	intent := jobIntent{Operation: request.Operation, ExpectedDeviceID: request.ExpectedDeviceID}
	switch request.Operation {
	case "backup":
		if request.Backup == nil || request.Restore != nil || request.Upgrade != nil || !filepath.IsAbs(request.Backup.Path) {
			return intent, errors.New("choose a destination for the encrypted backup")
		}
		intent.Path = request.Backup.Path
	case "restore.prepare":
		if request.Restore == nil || request.Backup != nil || request.Upgrade != nil || !filepath.IsAbs(request.Restore.Path) || request.Restore.ID != request.ID {
			return intent, errors.New("restore preparation requires its own stable ID and backup path")
		}
		intent.Path, intent.RestoreID = request.Restore.Path, request.Restore.ID
	case "restore.apply":
		if request.Restore == nil || request.Backup != nil || request.Upgrade != nil || !localID(request.Restore.ID) || request.Restore.Path != "" || request.Restore.Passphrase != "" {
			return intent, errors.New("choose an existing reviewed restore plan")
		}
		intent.RestoreID = request.Restore.ID
	case "upgrade.prepare", "upgrade.apply":
		if request.Upgrade == nil || !localID(request.Upgrade.ID) || request.Backup != nil || request.Restore != nil || (request.Operation == "upgrade.prepare" && request.Upgrade.ID != request.ID) {
			return intent, errors.New("upgrade requires its own stable preparation ID or an existing reviewed plan")
		}
		intent.UpgradeID = request.Upgrade.ID
	default:
		return intent, errors.New("this operation cannot run as a background job")
	}
	return intent, nil
}

func activeJob(state string) bool { return state == "starting" || state == "running" }
func (m *Manager) jobPath(id string) string {
	return filepath.Join(m.options.Root, "operations", id+".json")
}
func (m *Manager) jobLease(id string) (*lifecycle.Lock, error) {
	return lifecycle.Acquire(filepath.Join(m.options.Root, "operations", id))
}
func (m *Manager) jobRegistry() (*lifecycle.Lock, error) {
	if err := os.MkdirAll(m.options.Root, 0700); err != nil {
		return nil, err
	}
	if err := m.ensureRestoreArea("operations"); err != nil {
		return nil, err
	}
	return lifecycle.Acquire(filepath.Join(m.options.Root, "operations", "registry"))
}

func (m *Manager) waitJobRegistry(ctx context.Context) (*lifecycle.Lock, error) {
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		lock, err := m.jobRegistry()
		if err == nil {
			return lock, nil
		}
		select {
		case <-deadline.Done():
			return nil, errors.New("local operation records are busy or unavailable; refresh their status")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func decodeJobJSON(path string, value any) error {
	return decodeLocalJSON(path, 1<<20, value)
}

func decodeLocalJSON(path string, limit int64, value any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return errors.New("invalid local operation record")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return errors.New("local record exceeds its size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid local operation record")
	}
	return nil
}

func (m *Manager) loadJob(id string) (jobRecord, error) {
	var record jobRecord
	if !localID(id) {
		return record, errors.New("invalid local operation ID")
	}
	if err := decodeJobJSON(m.jobPath(id), &record); err != nil {
		return record, err
	}
	if record.Format != 1 || record.Info.ID != id || record.Intent.Operation != record.Info.Operation {
		return record, errors.New("invalid local operation identity")
	}
	return record, nil
}

func (m *Manager) latestJob() (string, error) {
	var pointer struct {
		ID string `json:"id"`
	}
	err := decodeJobJSON(filepath.Join(m.options.Root, "operations", "current.json"), &pointer)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !localID(pointer.ID) {
		return "", errors.New("invalid current local operation")
	}
	return pointer.ID, nil
}

func (m *Manager) saveJob(record jobRecord) error {
	return replaceJSON(m.jobPath(record.Info.ID), record)
}

// observeJob is called with the registry lock. A free worker lease confirms
// that a missing result cannot still be written by that worker. Do not replay.
func (m *Manager) observeJob(ctx context.Context, id string) (jobRecord, error) {
	record, err := m.loadJob(id)
	if err != nil {
		return record, err
	}
	if _, err := os.Stat(filepath.Join(m.options.Root, "operations", id+".cancel")); err == nil {
		record.Info.CancelRequested = true
	}
	if !activeJob(record.Info.State) {
		return record, nil
	}
	lease, err := m.jobLease(id)
	if err != nil {
		return record, nil
	}
	defer lease.Close()
	// A worker can finish between the first read and acquisition of its lease.
	// Re-read under that lease before deriving or writing an unknown outcome.
	record, err = m.loadJob(id)
	if err != nil || !activeJob(record.Info.State) {
		return record, err
	}
	if _, err := os.Stat(filepath.Join(m.options.Root, "operations", id+".cancel")); err == nil {
		record.Info.CancelRequested = true
	}
	created, err := time.Parse(time.RFC3339Nano, record.Info.CreatedAt)
	if err != nil {
		return record, errors.New("invalid local operation timestamp")
	}
	if record.Info.State == "starting" && time.Since(created) < 30*time.Second {
		return record, nil
	}
	// Restore's atomic plan/profile records can prove completion even if the
	// worker died before saving its last observation. An archive alone cannot.
	if record.Intent.RestoreID != "" {
		result, observeErr := m.restoreStatus(ctx, &RestoreRequest{ID: record.Intent.RestoreID})
		if observeErr == nil && result.Restore != nil && (record.Info.Operation == "restore.prepare" || result.Restore.Applied) {
			record.Info.State, record.Info.Restore = "succeeded", result.Restore
		}
	}
	if record.Intent.UpgradeID != "" {
		result, observeErr := m.upgradeStatus(ctx, &UpgradeRequest{ID: record.Intent.UpgradeID})
		if observeErr == nil && result.Upgrade != nil && (record.Info.Operation == "upgrade.prepare" || result.Upgrade.Applied) {
			record.Info.State, record.Info.Upgrade = "succeeded", result.Upgrade
		}
	}
	if record.Info.State != "succeeded" {
		record.Info.State = "unknown"
		record.Info.Error = "the operation worker ended without a confirmed result; the operation was not replayed"
	}
	record.Info.UpdatedAt = protocol.Now()
	return record, m.saveJob(record)
}

func (m *Manager) handleJob(ctx context.Context, request Request) (Result, error) {
	registry, err := m.waitJobRegistry(ctx)
	if err != nil {
		return Result{}, err
	}
	defer registry.Close()
	var info *JobInfo
	switch request.Operation {
	case "job.start":
		value, err := m.startJob(ctx, request.Job)
		if err != nil {
			return Result{}, err
		}
		info = &value
	case "job.status", "job.cancel":
		id := ""
		if request.Job != nil {
			id = request.Job.ID
		}
		if id == "" {
			if request.Operation == "job.cancel" {
				return Result{}, errors.New("choose the operation to cancel")
			}
			id, err = m.latestJob()
			if err != nil {
				return Result{}, err
			}
		}
		if id != "" {
			record, err := m.observeJob(ctx, id)
			if errors.Is(err, os.ErrNotExist) && request.Operation == "job.status" {
				break
			}
			if err != nil {
				return Result{}, err
			}
			if request.Operation == "job.cancel" && activeJob(record.Info.State) {
				path := filepath.Join(m.options.Root, "operations", id+".cancel")
				if err = createFile(path, []byte("cancel\n")); err != nil && !errors.Is(err, os.ErrExist) {
					return Result{}, err
				}
				record.Info.CancelRequested = true
			}
			info = &record.Info
		}
	default:
		return Result{}, errors.New("unsupported local operation command")
	}
	snapshot, statusErr := m.status(ctx)
	if statusErr != nil {
		snapshot = Snapshot{State: "unreadable", Projects: []protocol.Project{}, Message: statusErr.Error()}
	}
	return Result{Snapshot: snapshot, Job: info}, nil
}

// startJob holds the registry lock through process creation and stdin delivery.
// A repeated ID returns observations, including unknown ones; it never spawns.
func (m *Manager) startJob(ctx context.Context, request *JobRequest) (JobInfo, error) {
	intent, err := intentFor(request)
	if err != nil {
		return JobInfo{}, err
	}
	if record, err := m.observeJob(ctx, request.ID); err == nil {
		if record.Intent != intent {
			return JobInfo{}, errors.New("this operation ID already belongs to a different request")
		}
		return record.Info, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return JobInfo{}, err
	}
	previous, err := m.latestJob()
	if err != nil {
		return JobInfo{}, err
	}
	if previous != "" {
		record, err := m.observeJob(ctx, previous)
		if err != nil {
			return JobInfo{}, err
		}
		if activeJob(record.Info.State) {
			return JobInfo{}, errors.New("another local operation is still running; observe or cancel it first")
		}
	}
	program, err := runtimefiles.Program(m.options.Root, m.options.Program)
	if err != nil {
		return JobInfo{}, err
	}
	if err = ctx.Err(); err != nil {
		return JobInfo{}, err
	}
	info := JobInfo{ID: request.ID, Operation: request.Operation, State: "starting", CreatedAt: protocol.Now(), UpdatedAt: protocol.Now(), Progress: OperationProgress{Phase: "starting"}}
	record := jobRecord{Format: 1, Intent: intent, Info: info}
	if err = createJSON(m.jobPath(request.ID), record); err != nil {
		return JobInfo{}, err
	}
	pointer := filepath.Join(m.options.Root, "operations", "current.json")
	value := struct {
		ID string `json:"id"`
	}{request.ID}
	if previous == "" {
		err = createJSON(pointer, value)
	} else {
		err = replaceJSON(pointer, value)
	}
	if err != nil {
		return JobInfo{}, err
	}
	command := exec.Command(program, "desktop-job", "-root", m.options.Root)
	if m.options.Engine != "" {
		command.Args = append(command.Args, "-engine-binary", m.options.Engine)
	}
	command.Dir, command.Stdout, command.Stderr = m.options.Root, io.Discard, io.Discard
	detach(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return JobInfo{}, err
	}
	if err = command.Start(); err != nil {
		stdin.Close()
		record.Info.State, record.Info.Error = "failed", "the local operation worker could not be started"
		record.Info.UpdatedAt = protocol.Now()
		if saveErr := m.saveJob(record); saveErr != nil {
			return JobInfo{}, saveErr
		}
		return record.Info, nil
	}
	go func() { _ = command.Wait() }()
	err = json.NewEncoder(stdin).Encode(request)
	closeErr := stdin.Close()
	if err != nil || closeErr != nil {
		return JobInfo{}, errors.New("operation delivery is unconfirmed; query the original operation ID before continuing")
	}
	return info, nil
}

// RunJob is the detached worker entry point. Only this worker writes progress.
// The lease lets later inputs distinguish a live worker from an unknown result.
func (m *Manager) RunJob(parent context.Context, request JobRequest) error {
	intent, err := intentFor(&request)
	if err != nil {
		return err
	}
	lease, err := m.jobLease(request.ID)
	if err != nil {
		return errors.New("this operation already has a worker")
	}
	defer lease.Close()
	ctx, cancel := context.WithTimeout(parent, 24*time.Hour)
	defer cancel()
	registry, err := m.waitJobRegistry(ctx)
	if err != nil {
		return err
	}
	record, err := m.loadJob(request.ID)
	latest, latestErr := m.latestJob()
	created, timeErr := time.Parse(time.RFC3339Nano, record.Info.CreatedAt)
	if err != nil || latestErr != nil || timeErr != nil || latest != request.ID || record.Intent != intent || record.Info.State != "starting" || time.Since(created) > 30*time.Second {
		registry.Close()
		return errors.New("the operation intent is no longer available to this worker")
	}
	record.Info.State, record.Info.UpdatedAt = "running", protocol.Now()
	err = m.saveJob(record)
	registry.Close()
	if err != nil {
		return err
	}
	cancelPath := filepath.Join(m.options.Root, "operations", request.ID+".cancel")
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(cancelPath); err == nil {
				cancel()
				return
			}
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	worker := *m
	lastReport := time.Time{}
	var persistErr error
	worker.options.Report = func(value OperationProgress) {
		changed := record.Info.Progress.Phase != value.Phase
		record.Info.Progress = value
		if changed || time.Since(lastReport) >= 250*time.Millisecond {
			record.Info.UpdatedAt = protocol.Now()
			if persistErr == nil {
				persistErr = m.saveJob(record)
			}
			lastReport = time.Now()
		}
		if persistErr != nil {
			cancel()
		}
		if m.options.Report != nil {
			m.options.Report(value)
		}
		if _, err := os.Stat(cancelPath); err == nil {
			cancel()
		}
	}
	if _, err := os.Stat(cancelPath); err == nil {
		cancel()
	}
	result, operationErr := worker.Handle(ctx, Request{Operation: request.Operation, ExpectedDeviceID: request.ExpectedDeviceID, Backup: request.Backup, Restore: request.Restore, Upgrade: request.Upgrade})
	record.Info.UpdatedAt = protocol.Now()
	record.Info.Backup, record.Info.Restore = result.Backup, result.Restore
	record.Info.Upgrade = result.Upgrade
	_, cancelErr := os.Stat(cancelPath)
	record.Info.CancelRequested = cancelErr == nil
	switch {
	case operationErr == nil:
		// A cancellation arriving after the atomic commit cannot undo success.
		record.Info.State, record.Info.Progress.Phase = "succeeded", "complete"
	case persistErr != nil:
		record.Info.State, record.Info.Error = "failed", "operation progress could not be retained; inspect the local state"
	case errors.Is(ctx.Err(), context.Canceled):
		record.Info.State = "cancelled"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		record.Info.State, record.Info.Error = "failed", "the local operation exceeded its time limit"
	default:
		record.Info.State, record.Info.Error = "failed", operationErr.Error()
	}
	return m.saveJob(record)
}
