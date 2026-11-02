package localnode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mewdex/internal/protocol"
)

func seedJob(t *testing.T, m *Manager, request JobRequest, created string) {
	t.Helper()
	registry, err := m.jobRegistry()
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	intent, err := intentFor(&request)
	if err != nil {
		t.Fatal(err)
	}
	info := JobInfo{ID: request.ID, Operation: request.Operation, State: "starting", CreatedAt: created, UpdatedAt: created, Progress: OperationProgress{Phase: "starting"}}
	if err = createJSON(m.jobPath(request.ID), jobRecord{Format: 1, Intent: intent, Info: info}); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(m.options.Root, "operations", "current.json")
	value := struct {
		ID string `json:"id"`
	}{request.ID}
	if _, err = os.Stat(pointer); os.IsNotExist(err) {
		err = createJSON(pointer, value)
	} else if err == nil {
		err = replaceJSON(pointer, value)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func readJob(t *testing.T, m *Manager, id string) *JobInfo {
	t.Helper()
	result, err := m.Handle(context.Background(), Request{Operation: "job.status", Job: &JobRequest{ID: id}})
	if err != nil || result.Job == nil {
		t.Fatalf("operation observation failed: %v", err)
	}
	return result.Job
}

func TestCompletedJobIsRecoverableAndSameIDNeverRepeatsTheBackup(t *testing.T) {
	m, _, c := hubBackup(t)
	path := filepath.Join(t.TempDir(), "background.age")
	request := JobRequest{ID: protocol.ID(), Operation: "backup", ExpectedDeviceID: c.DeviceID, Backup: &BackupRequest{Path: path, Passphrase: restorePassword}}
	seedJob(t, m, request, protocol.Now())
	if err := m.RunJob(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	job := readJob(t, m, "")
	if job.State != "succeeded" || job.Backup == nil || job.Backup.Path != path {
		t.Fatal("completed operation lost its output")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	newInput, err := New(m.options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := newInput.Handle(context.Background(), Request{Operation: "job.start", Job: &request})
	if err != nil || result.Job == nil || result.Job.State != "succeeded" {
		t.Fatalf("original operation was not recovered: %v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("observing the same operation rewrote its encrypted output")
	}
	request.Backup.Path = filepath.Join(t.TempDir(), "another.age")
	if _, err = newInput.Handle(context.Background(), Request{Operation: "job.start", Job: &request}); err == nil {
		t.Fatal("an existing operation ID accepted another destination")
	}
	if err = filepath.WalkDir(filepath.Join(m.options.Root, "operations"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(value, []byte(restorePassword)) {
			t.Fatal("an archive passphrase was persisted in operation state")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLostWorkerRemainsUnknownAndCannotResumeFromTheSameID(t *testing.T) {
	m := testManager(t)
	request := JobRequest{ID: protocol.ID(), Operation: "backup", Backup: &BackupRequest{Path: filepath.Join(t.TempDir(), "absent.age"), Passphrase: restorePassword}}
	seedJob(t, m, request, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	lease, err := m.jobLease(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if readJob(t, m, request.ID).State != "starting" {
		t.Fatal("a worker holding its lease was assumed dead")
	}
	lease.Close()
	if readJob(t, m, request.ID).State != "unknown" {
		t.Fatal("an absent worker was treated as a confirmed failure")
	}
	if err = m.RunJob(context.Background(), request); err == nil {
		t.Fatal("a late worker executed an unknown operation")
	}
	result, err := m.Handle(context.Background(), Request{Operation: "job.start", Job: &request})
	if err != nil || result.Job == nil || result.Job.State != "unknown" {
		t.Fatal("same-ID retry did not retain the unknown outcome")
	}
	if _, err = os.Stat(request.Backup.Path); !os.IsNotExist(err) {
		t.Fatal("unknown backup was replayed")
	}
}

func TestJobObservationWaitsForAnActiveRecordWriter(t *testing.T) {
	m := testManager(t)
	registry, err := m.jobRegistry()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := m.Handle(ctx, Request{Operation: "job.status", Job: &JobRequest{ID: protocol.ID()}})
		if err == nil && result.Job != nil {
			err = errors.New("an unaccepted operation was reported as a running job")
		}
		done <- err
	}()
	select {
	case err := <-done:
		registry.Close()
		t.Fatalf("observation returned before the record writer released its lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	registry.Close()
	if err = <-done; err != nil {
		t.Fatalf("normal record contention prevented a read: %v", err)
	}
}

func TestCancellationDuringBackupRemovesOnlyItsPartialOutput(t *testing.T) {
	m, _, c := hubBackup(t)
	attachment := filepath.Join(c.DataDir, "attachments", "large")
	if err := os.MkdirAll(filepath.Dir(attachment), 0700); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("preserved original attachment\n"), 200000)
	if err := os.WriteFile(attachment, content, 0600); err != nil {
		t.Fatal(err)
	}
	profile, _ := os.ReadFile(m.profile())
	request := JobRequest{ID: protocol.ID(), Operation: "backup", Backup: &BackupRequest{Path: filepath.Join(t.TempDir(), "cancelled.age"), Passphrase: restorePassword}}
	seedJob(t, m, request, protocol.Now())
	cancelled := false
	m.options.Report = func(value OperationProgress) {
		if !cancelled && value.Phase == "writing" && value.Bytes > 0 {
			cancelled = true
			if _, err := m.Handle(context.Background(), Request{Operation: "job.cancel", Job: &JobRequest{ID: request.ID}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := m.RunJob(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if !cancelled || readJob(t, m, request.ID).State != "cancelled" {
		t.Fatal("streaming cancellation was not observed")
	}
	if _, err := os.Stat(request.Backup.Path); !os.IsNotExist(err) {
		t.Fatal("cancelled backup retained its partial destination")
	}
	actual, _ := os.ReadFile(attachment)
	current, _ := os.ReadFile(m.profile())
	if !bytes.Equal(content, actual) || !bytes.Equal(profile, current) {
		t.Fatal("cancelling a backup changed its source state")
	}
}

func TestRestoreJobRebuildsReviewAndConfirmsCommitDespiteLateCancellation(t *testing.T) {
	m, archive, _ := hubBackup(t)
	prepare := JobRequest{ID: protocol.ID(), Operation: "restore.prepare"}
	prepare.Restore = &RestoreRequest{ID: prepare.ID, Path: archive, Passphrase: restorePassword}
	seedJob(t, m, prepare, protocol.Now())
	if err := m.RunJob(context.Background(), prepare); err != nil {
		t.Fatal(err)
	}
	plan := readJob(t, m, prepare.ID).Restore
	if plan == nil || plan.Applied {
		t.Fatal("restore preparation skipped its review")
	}
	// Simulate a lost final observation after the durable plan was written.
	record, _ := m.loadJob(prepare.ID)
	record.Info.State, record.Info.Restore = "running", nil
	if err := m.saveJob(record); err != nil {
		t.Fatal(err)
	}
	if recovered := readJob(t, m, prepare.ID); recovered.State != "succeeded" || recovered.Restore == nil {
		t.Fatal("a durable prepared plan was not recovered after worker loss")
	}
	apply := JobRequest{ID: protocol.ID(), Operation: "restore.apply", Restore: &RestoreRequest{ID: plan.ID}}
	seedJob(t, m, apply, protocol.Now())
	m.options.Report = func(value OperationProgress) {
		if value.Phase == "committing" {
			if _, err := m.Handle(context.Background(), Request{Operation: "job.cancel", Job: &JobRequest{ID: apply.ID}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := m.RunJob(context.Background(), apply); err != nil {
		t.Fatal(err)
	}
	job := readJob(t, m, apply.ID)
	if job.State != "succeeded" || !job.CancelRequested || job.Restore == nil || !job.Restore.Applied {
		t.Fatal("a completed profile switch was reported cancelled")
	}
	if strings.Contains(job.Error, restorePassword) {
		t.Fatal("operation diagnostics exposed its passphrase")
	}
}

func TestCancellationDuringRestorePreparationPreservesOriginalState(t *testing.T) {
	m, archive, c := hubBackup(t)
	profile, _ := os.ReadFile(m.profile())
	database, _ := os.ReadFile(filepath.Join(c.DataDir, "hub.db"))
	request := JobRequest{ID: protocol.ID(), Operation: "restore.prepare"}
	request.Restore = &RestoreRequest{ID: request.ID, Path: archive, Passphrase: restorePassword}
	seedJob(t, m, request, protocol.Now())
	m.options.Report = func(value OperationProgress) {
		if value.Phase == "preparing-state" {
			if _, err := m.Handle(context.Background(), Request{Operation: "job.cancel", Job: &JobRequest{ID: request.ID}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := m.RunJob(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if readJob(t, m, request.ID).State != "cancelled" {
		t.Fatal("restore preparation did not observe cancellation")
	}
	if _, err := os.Stat(m.restoreDirectory(request.ID)); !os.IsNotExist(err) {
		t.Fatal("cancelled preparation retained its partially transformed state")
	}
	if _, err := os.Stat(m.restoreRecord(request.ID)); !os.IsNotExist(err) {
		t.Fatal("cancelled preparation exposed an adoptable plan")
	}
	current, _ := os.ReadFile(m.profile())
	actual, _ := os.ReadFile(filepath.Join(c.DataDir, "hub.db"))
	if !bytes.Equal(current, profile) || !bytes.Equal(actual, database) {
		t.Fatal("cancelling preparation changed the original profile or data")
	}
}
